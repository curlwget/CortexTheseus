// Copyright 2023 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64)

//go:generate go run generator.go

// Package libc is the runtime for programs generated by ccgo/v4 or later.
//
// # Version compatibility
//
// The API of this package, in particular the bits that directly support the
// ccgo compiler, may change in a way that is not backward compatible. If you
// have generated some Go code from C you should stick to the version of this
// package that you used at that time and was tested with your payload. The
// correct way to upgrade to a newer version of this package is to first
// recompile (C to Go) your code with a newwer version if ccgo that depends on
// the new libc version.
//
// If you use C to Go translated code provided by others, stick to the version
// of libc that translated code shows in its go.mod file and do not upgrade the
// dependency just because a newer libc is tagged.Vgq
//
// This is if course unfortunate. However, it's somewhat similar to C code
// linked with a specific version of, say GNU libc. When such code asking for
// glibc5 is run on a system with glibc6, or vice versa, it will fail.
//
// As a particular example, if your project imports modernc.org/sqlite you
// should use the same libc version as seen in the go.mod file of the sqlite
// package.
//
// tl;dr: It is not always possible to fix ccgo bugs and/or improve performance
// of the ccgo transpiled code without occasionally making incompatible changes
// to this package.
//
// # Thread Local Storage
//
// A TLS instance represents a main thread or a thread created by
// Xpthread_create. A TLS instance is not safe for concurrent use by multiple
// goroutines.
//
// If a program starts the C main function, a TLS instance is created
// automatically and the goroutine entering main() is locked to the OS thread.
// The translated C code then may create other pthreads by calling
// Xpthread_create.
//
// If the translated C code is part of a library package, new TLS instances
// must be created manually in user/client code. The first TLS instance created
// will be the "main" libc thread, but it will be not locked to OS thread
// automatically. Any subsequently manually created TLS instances will call
// Xpthread_create, but without spawning a new goroutine.
//
// A manual call to Xpthread_create will create a new TLS instance automatically
// and spawn a new goroutine executing the thread function.

// Package libc provides run time support for programs generated by the
// [ccgo] C to Go transpiler, version 4 or later.
//
// # Concurrency
//
// Many C libc functions are not thread safe. Such functions are not safe
// for concurrent use by multiple goroutines in the Go translation as well.
//
// # Thread Local Storage
//
// C threads are modeled as Go goroutines.  Every such C thread, ie. a Go
// goroutine, must use its own Thread Local Storage instance implemented by the
// [TLS] type.
//
// # Signals
//
// Signal handling in translated C code is not coordinated with the Go runtime.
// This is probably the same as when running C code via CGo.
//
// # Environmental variables
//
// This package synchronizes its environ with the current Go environ lazily and
// only once.
//
// # libc API documentation copyright
//
// From [Linux man-pages Copyleft]
//
//	Permission is granted to make and distribute verbatim copies of this
//	manual provided the copyright notice and this permission notice are
//	preserved on all copies.
//
//	Permission is granted to copy and distribute modified versions of this
//	manual under the conditions for verbatim copying, provided that the
//	entire resulting derived work is distributed under the terms of a
//	permission notice identical to this one.
//
//	Since the Linux kernel and libraries are constantly changing, this
//	manual page may be incorrect or out-of-date. The author(s) assume no
//	responsibility for errors or omissions, or for damages resulting from
//	the use of the information contained herein. The author(s) may not have
//	taken the same level of care in the production of this manual, which is
//	licensed free of charge, as they might when working professionally.
//
//	Formatted or processed versions of this manual, if unaccompanied by the
//	source, must acknowledge the copyright and authors of this work.
//
// [Linux man-pages Copyleft]: https://spdx.org/licenses/Linux-man-pages-copyleft.html
// [ccgo]: http://modernc.org/ccgo/v4
package libc // import "modernc.org/libc"

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	gosignal "os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	guuid "github.com/google/uuid"
	"golang.org/x/sys/unix"
	"modernc.org/libc/uuid/uuid"
	"modernc.org/memory"
)

var (
	_ error = (*MemAuditError)(nil)

	allocator   memory.Allocator
	allocatorMu sync.Mutex

	atExitMu sync.Mutex
	atExit   []func()

	tid atomic.Int32 // TLS Go ID

	Covered  = map[uintptr]struct{}{}
	CoveredC = map[string]struct{}{}
	coverPCs [1]uintptr //TODO not concurrent safe
)

func init() {
	nm, err := os.Executable()
	if err != nil {
		return
	}

	Xprogram_invocation_name = mustCString(nm)
	Xprogram_invocation_short_name = mustCString(filepath.Base(nm))
}

// RawMem64 represents the biggest uint64 array the runtime can handle.
type RawMem64 [unsafe.Sizeof(RawMem{}) / unsafe.Sizeof(uint64(0))]uint64

type MemAuditError struct {
	Caller  string
	Message string
}

func (e *MemAuditError) Error() string {
	return fmt.Sprintf("%s: %s", e.Caller, e.Message)
}

// Start executes C's main.
func Start(main func(*TLS, int32, uintptr) int32) {
	runtime.LockOSThread()
	if isMemBrk {
		defer func() {
			trc("==== PANIC")
			for _, v := range MemAudit() {
				trc("", v.Error())
			}
		}()
	}

	tls := NewTLS()
	Xexit(tls, main(tls, int32(len(os.Args)), mustAllocStrings(os.Args)))
}

func mustAllocStrings(a []string) (r uintptr) {
	nPtrs := len(a) + 1
	pPtrs := mustCalloc(Tsize_t(uintptr(nPtrs) * unsafe.Sizeof(uintptr(0))))
	ptrs := unsafe.Slice((*uintptr)(unsafe.Pointer(pPtrs)), nPtrs)
	nBytes := 0
	for _, v := range a {
		nBytes += len(v) + 1
	}
	pBytes := mustCalloc(Tsize_t(nBytes))
	b := unsafe.Slice((*byte)(unsafe.Pointer(pBytes)), nBytes)
	for i, v := range a {
		copy(b, v)
		b = b[len(v)+1:]
		ptrs[i] = pBytes
		pBytes += uintptr(len(v)) + 1
	}
	return pPtrs
}

func mustCString(s string) (r uintptr) {
	n := len(s)
	r = mustMalloc(Tsize_t(n + 1))
	copy(unsafe.Slice((*byte)(unsafe.Pointer(r)), n), s)
	*(*byte)(unsafe.Pointer(r + uintptr(n))) = 0
	return r
}

// CString returns a pointer to a zero-terminated version of s. The caller is
// responsible for freeing the allocated memory using Xfree.
func CString(s string) (uintptr, error) {
	n := len(s)
	p := Xmalloc(nil, Tsize_t(n)+1)
	if p == 0 {
		return 0, fmt.Errorf("CString: cannot allocate %d bytes", n+1)
	}

	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), n), s)
	*(*byte)(unsafe.Pointer(p + uintptr(n))) = 0
	return p, nil
}

// GoBytes returns a byte slice from a C char* having length len bytes.
func GoBytes(s uintptr, len int) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(s)), len)
}

// GoString returns the value of a C string at s.
func GoString(s uintptr) string {
	if s == 0 {
		return ""
	}

	var buf []byte
	for {
		b := *(*byte)(unsafe.Pointer(s))
		if b == 0 {
			return string(buf)
		}

		buf = append(buf, b)
		s++
	}
}

func mustMalloc(sz Tsize_t) (r uintptr) {
	if r = Xmalloc(nil, sz); r != 0 || sz == 0 {
		return r
	}

	panic(todo("OOM"))
}

func mustCalloc(sz Tsize_t) (r uintptr) {
	if r := Xcalloc(nil, 1, sz); r != 0 || sz == 0 {
		return r
	}

	panic(todo("OOM"))
}

type tlsStackSlot struct {
	p  uintptr
	sz Tsize_t
}

// TLS emulates thread local storage. TLS is not safe for concurrent use by
// multiple goroutines.
type TLS struct {
	allocaStack         []int
	allocas             []uintptr
	jumpBuffers         []uintptr
	pendingSignals      chan os.Signal
	pthread             uintptr // *t__pthread
	pthreadCleanupItems []pthreadCleanupItem
	pthreadKeyValues    map[Tpthread_key_t]uintptr
	sigHandlers         map[int32]uintptr
	sp                  int
	stack               []tlsStackSlot

	ID int32

	checkSignals bool
	ownsPthread  bool
}

var __ccgo_environOnce sync.Once

// NewTLS returns a newly created TLS that must be eventually closed to prevent
// resource leaks.
func NewTLS() (r *TLS) {
	id := tid.Add(1)
	if id == 0 {
		id = tid.Add(1)
	}
	__ccgo_environOnce.Do(func() {
		Xenviron = mustAllocStrings(os.Environ())
	})
	pthread := mustMalloc(Tsize_t(unsafe.Sizeof(t__pthread{})))
	*(*t__pthread)(unsafe.Pointer(pthread)) = t__pthread{
		Flocale: uintptr(unsafe.Pointer(&X__libc.Fglobal_locale)),
		Fself:   pthread,
		Ftid:    id,
	}
	return &TLS{
		ID:          id,
		ownsPthread: true,
		pthread:     pthread,
		sigHandlers: map[int32]uintptr{},
	}
}

// int *__errno_location(void)
func X__errno_location(tls *TLS) (r uintptr) {
	return tls.pthread + unsafe.Offsetof(t__pthread{}.Ferrno_val)
}

// int *__errno_location(void)
func X___errno_location(tls *TLS) (r uintptr) {
	return X__errno_location(tls)
}

func (tls *TLS) setErrno(n int32) {
	if tls == nil {
		return
	}

	*(*int32)(unsafe.Pointer(X__errno_location(tls))) = n
}

func (tls *TLS) String() string {
	return fmt.Sprintf("TLS#%v pthread=%x", tls.ID, tls.pthread)
}

// Alloc allocates n bytes in tls's local storage. Calls to Alloc() must be
// strictly paired with calls to TLS.Free on function exit. That also means any
// memory from Alloc must not be used after a function returns.
//
// The order matters. This is ok:
//
//	p := tls.Alloc(11)
//		q := tls.Alloc(22)
//		tls.Free(22)
//		// q is no more usable here.
//	tls.Free(11)
//	// p is no more usable here.
//
// This is not correct:
//
//	tls.Alloc(11)
//		tls.Alloc(22)
//		tls.Free(11)
//	tls.Free(22)
func (tls *TLS) Alloc(n0 int) (r uintptr) {
	// shrink	stats									speedtest1
	// -----------------------------------------------------------------------------------------------
	//    0		total  2,544, nallocs 107,553,070, nmallocs 25, nreallocs 107,553,045	10.984s
	//    1		total  2,544, nallocs 107,553,070, nmallocs 25, nreallocs  38,905,980	 9.597s
	//    2		total  2,616, nallocs 107,553,070, nmallocs 25, nreallocs  18,201,284	 9.206s
	//    3		total  2,624, nallocs 107,553,070, nmallocs 25, nreallocs  16,716,302	 9.155s
	//    4		total  2,624, nallocs 107,553,070, nmallocs 25, nreallocs  16,156,102	 9.398s
	//    8		total  3,408, nallocs 107,553,070, nmallocs 25, nreallocs  14,364,274	 9.198s
	//   16		total  3,976, nallocs 107,553,070, nmallocs 25, nreallocs   6,219,602	 8.910s
	// ---------------------------------------------------------------------------------------------
	//   32		total  5,120, nallocs 107,553,070, nmallocs 25, nreallocs   1,089,037	 8.836s
	// ---------------------------------------------------------------------------------------------
	//   64		total  6,520, nallocs 107,553,070, nmallocs 25, nreallocs       1,788	 8.420s
	//  128		total  8,848, nallocs 107,553,070, nmallocs 25, nreallocs       1,098	 8.833s
	//  256		total  8,848, nallocs 107,553,070, nmallocs 25, nreallocs       1,049	 9.508s
	//  512		total 33,336, nallocs 107,553,070, nmallocs 25, nreallocs          88	 8.667s
	// none		total 33,336, nallocs 107,553,070, nmallocs 25, nreallocs          88	 8.408s
	const shrinkSegment = 32
	n := Tsize_t(n0)
	if tls.sp < len(tls.stack) {
		p := tls.stack[tls.sp].p
		sz := tls.stack[tls.sp].sz
		if sz >= n /* && sz <= shrinkSegment*n */ {
			// Segment shrinking is nice to have but Tcl does some dirty hacks in coroutine
			// handling that require stability of stack addresses, out of the C execution
			// model. Disabled.
			tls.sp++
			return p
		}

		Xfree(tls, p)
		r = mustMalloc(n)
		tls.stack[tls.sp] = tlsStackSlot{p: r, sz: Xmalloc_usable_size(tls, r)}
		tls.sp++
		return r

	}

	r = mustMalloc(n)
	tls.stack = append(tls.stack, tlsStackSlot{p: r, sz: Xmalloc_usable_size(tls, r)})
	tls.sp++
	return r
}

// Free manages memory of the preceding TLS.Alloc()
func (tls *TLS) Free(n int) {
	//TODO shrink stacks if possible. Tcl is currently against.
	tls.sp--
	if !tls.checkSignals {
		return
	}

	select {
	case sig := <-tls.pendingSignals:
		signum := int32(sig.(syscall.Signal))
		h, ok := tls.sigHandlers[signum]
		if !ok {
			break
		}

		switch h {
		case SIG_DFL:
			// nop
		case SIG_IGN:
			// nop
		default:
			(*(*func(*TLS, int32))(unsafe.Pointer(&struct{ uintptr }{h})))(tls, signum)
		}
	default:
		// nop
	}
}

func (tls *TLS) alloca(n Tsize_t) (r uintptr) {
	r = mustMalloc(n)
	tls.allocas = append(tls.allocas, r)
	return r
}

// AllocaEntry must be called early on function entry when the function calls
// or may call alloca(3).
func (tls *TLS) AllocaEntry() {
	tls.allocaStack = append(tls.allocaStack, len(tls.allocas))
}

// AllocaExit must be defer-called on function exit when the function calls or
// may call alloca(3).
func (tls *TLS) AllocaExit() {
	n := len(tls.allocaStack)
	x := tls.allocaStack[n-1]
	tls.allocaStack = tls.allocaStack[:n-1]
	for _, v := range tls.allocas[x:] {
		Xfree(tls, v)
	}
	tls.allocas = tls.allocas[:x]
}

func (tls *TLS) Close() {
	defer func() { *tls = TLS{} }()

	for _, v := range tls.allocas {
		Xfree(tls, v)
	}
	for _, v := range tls.stack /* shrink diabled[:tls.sp] */ {
		Xfree(tls, v.p)
	}
	if tls.ownsPthread {
		Xfree(tls, tls.pthread)
	}
}

func (tls *TLS) PushJumpBuffer(jb uintptr) {
	tls.jumpBuffers = append(tls.jumpBuffers, jb)
}

type LongjmpRetval int32

func (tls *TLS) PopJumpBuffer(jb uintptr) {
	n := len(tls.jumpBuffers)
	if n == 0 || tls.jumpBuffers[n-1] != jb {
		panic(todo("unsupported setjmp/longjmp usage"))
	}

	tls.jumpBuffers = tls.jumpBuffers[:n-1]
}

func (tls *TLS) Longjmp(jb uintptr, val int32) {
	tls.PopJumpBuffer(jb)
	if val == 0 {
		val = 1
	}
	panic(LongjmpRetval(val))
}

// ============================================================================

func Xexit(tls *TLS, code int32) {
	//TODO atexit finalizers
	X__stdio_exit(tls)
	for _, v := range atExit {
		v()
	}
	atExitHandlersMu.Lock()
	for _, v := range atExitHandlers {
		(*(*func(*TLS))(unsafe.Pointer(&struct{ uintptr }{v})))(tls)
	}
	os.Exit(int(code))
}

func _exit(tls *TLS, code int32) {
	Xexit(tls, code)
}

var abort Tsigaction

func Xabort(tls *TLS) {
	X__libc_sigaction(tls, SIGABRT, uintptr(unsafe.Pointer(&abort)), 0)
	unix.Kill(unix.Getpid(), syscall.Signal(SIGABRT))
	panic(todo("unrechable"))
}

type lock struct {
	sync.Mutex
	waiters int
}

var (
	locksMu sync.Mutex
	locks   = map[uintptr]*lock{}
)

/*

	T1		T2

	lock(&foo)			// foo: 0 -> 1

			lock(&foo)	// foo: 1 -> 2

	unlock(&foo)			// foo: 2 -> 1, non zero means waiter(s) active

			unlock(&foo)	// foo: 1 -> 0

*/

func ___lock(tls *TLS, p uintptr) {
	if atomic.AddInt32((*int32)(unsafe.Pointer(p)), 1) == 1 {
		return
	}

	// foo was already acquired by some other C thread.
	locksMu.Lock()
	l := locks[p]
	if l == nil {
		l = &lock{}
		locks[p] = l
		l.Lock()
	}
	l.waiters++
	locksMu.Unlock()
	l.Lock() // Wait for T1 to release foo. (X below)
}

func ___unlock(tls *TLS, p uintptr) {
	if atomic.AddInt32((*int32)(unsafe.Pointer(p)), -1) == 0 {
		return
	}

	// Some other C thread is waiting for foo.
	locksMu.Lock()
	l := locks[p]
	if l == nil {
		// We are T1 and we got the locksMu locked before T2.
		l = &lock{waiters: 1}
		l.Lock()
	}
	l.Unlock() // Release foo, T2 may now lock it. (X above)
	l.waiters--
	if l.waiters == 0 { // we are T2
		delete(locks, p)
	}
	locksMu.Unlock()
}

type lockedFile struct {
	ch      chan struct{}
	waiters int
}

var (
	lockedFilesMu sync.Mutex
	lockedFiles   = map[uintptr]*lockedFile{}
)

func X__lockfile(tls *TLS, file uintptr) int32 {
	return ___lockfile(tls, file)
}

// int __lockfile(FILE *f)
func ___lockfile(tls *TLS, file uintptr) int32 {
	panic(todo(""))
	// lockedFilesMu.Lock()

	// defer lockedFilesMu.Unlock()

	// l := lockedFiles[file]
	// if l == nil {
	// 	l = &lockedFile{ch: make(chan struct{}, 1)}
	// 	lockedFiles[file] = l
	// }

	// l.waiters++
	// l.ch <- struct{}{}
}

func X__unlockfile(tls *TLS, file uintptr) {
	___unlockfile(tls, file)
}

// void __unlockfile(FILE *f)
func ___unlockfile(tls *TLS, file uintptr) {
	panic(todo(""))
	lockedFilesMu.Lock()

	defer lockedFilesMu.Unlock()

	l := lockedFiles[file]
	l.waiters--
	if l.waiters == 0 {
		delete(lockedFiles, file)
	}
	<-l.ch
}

// void __synccall(void (*func)(void *), void *ctx)
func ___synccall(tls *TLS, fn, ctx uintptr) {
	(*(*func(*TLS, uintptr))(unsafe.Pointer(&struct{ uintptr }{fn})))(tls, ctx)
}

func ___randname(tls *TLS, template uintptr) (r1 uintptr) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var i int32
	var r uint64
	var _ /* ts at bp+0 */ Ttimespec
	X__clock_gettime(tls, CLOCK_REALTIME, bp)
	goto _2
_2:
	r = uint64((*(*Ttimespec)(unsafe.Pointer(bp))).Ftv_sec+(*(*Ttimespec)(unsafe.Pointer(bp))).Ftv_nsec) + uint64(tls.ID)*uint64(65537)
	i = 0
	for {
		if !(i < int32(6)) {
			break
		}
		*(*int8)(unsafe.Pointer(template + uintptr(i))) = int8(uint64('A') + r&uint64(15) + r&uint64(16)*uint64(2))
		goto _3
	_3:
		i++
		r >>= uint64(5)
	}
	return template
}

func ___get_tp(tls *TLS) uintptr {
	return tls.pthread
}

func Xfork(t *TLS) int32 {
	if __ccgo_strace {
		trc("t=%v, (%v:)", t, origin(2))
	}
	t.setErrno(ENOSYS)
	return -1
}

const SIG_DFL = 0
const SIG_IGN = 1

func Xsignal(tls *TLS, signum int32, handler uintptr) (r uintptr) {
	r, tls.sigHandlers[signum] = tls.sigHandlers[signum], handler
	switch handler {
	case SIG_DFL:
		gosignal.Reset(syscall.Signal(signum))
	case SIG_IGN:
		gosignal.Ignore(syscall.Signal(signum))
	default:
		if tls.pendingSignals == nil {
			tls.pendingSignals = make(chan os.Signal, 3)
			tls.checkSignals = true
		}
		gosignal.Notify(tls.pendingSignals, syscall.Signal(signum))
	}
	return r
}

var (
	atExitHandlersMu sync.Mutex
	atExitHandlers   []uintptr
)

func Xatexit(tls *TLS, func_ uintptr) (r int32) {
	atExitHandlersMu.Lock()
	atExitHandlers = append(atExitHandlers, func_)
	atExitHandlersMu.Unlock()
	return 0
}

var __sync_synchronize_dummy int32

// __sync_synchronize();
func X__sync_synchronize(t *TLS) {
	if __ccgo_strace {
		trc("t=%v, (%v:)", t, origin(2))
	}
	// Attempt to implement a full memory barrier without assembler.
	atomic.StoreInt32(&__sync_synchronize_dummy, atomic.LoadInt32(&__sync_synchronize_dummy)+1)
}

func Xdlopen(t *TLS, filename uintptr, flags int32) uintptr {
	if __ccgo_strace {
		trc("t=%v filename=%v flags=%v, (%v:)", t, filename, flags, origin(2))
	}
	return 0
}

func Xdlsym(t *TLS, handle, symbol uintptr) uintptr {
	if __ccgo_strace {
		trc("t=%v symbol=%v, (%v:)", t, symbol, origin(2))
	}
	return 0
}

var dlErrorMsg = []byte("not supported\x00")

func Xdlerror(t *TLS) uintptr {
	if __ccgo_strace {
		trc("t=%v, (%v:)", t, origin(2))
	}
	return uintptr(unsafe.Pointer(&dlErrorMsg[0]))
}

func Xdlclose(t *TLS, handle uintptr) int32 {
	if __ccgo_strace {
		trc("t=%v handle=%v, (%v:)", t, handle, origin(2))
	}
	panic(todo(""))
}

func Xsystem(t *TLS, command uintptr) int32 {
	if __ccgo_strace {
		trc("t=%v command=%v, (%v:)", t, command, origin(2))
	}
	s := GoString(command)
	if command == 0 {
		panic(todo(""))
	}

	cmd := exec.Command("sh", "-c", s)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		ps := err.(*exec.ExitError)
		return int32(ps.ExitCode())
	}

	return 0
}

func Xsched_yield(tls *TLS) int32 {
	runtime.Gosched()
	return 0
}

// AtExit will attempt to run f at process exit. The execution cannot be
// guaranteed, neither its ordering with respect to any other handlers
// registered by AtExit.
func AtExit(f func()) {
	atExitMu.Lock()
	atExit = append(atExit, f)
	atExitMu.Unlock()
}

func Bool64(b bool) int64 {
	if b {
		return 1
	}

	return 0
}

func Environ() uintptr {
	__ccgo_environOnce.Do(func() {
		Xenviron = mustAllocStrings(os.Environ())
	})
	return Xenviron
}

func EnvironP() uintptr {
	__ccgo_environOnce.Do(func() {
		Xenviron = mustAllocStrings(os.Environ())
	})
	return uintptr(unsafe.Pointer(&Xenviron))
}

// NewVaList is like VaList but automatically allocates the correct amount of
// memory for all of the items in args.
//
// The va_list return value is used to pass the constructed var args to var
// args accepting functions. The caller of NewVaList is responsible for freeing
// the va_list.
func NewVaList(args ...interface{}) (va_list uintptr) {
	return VaList(NewVaListN(len(args)), args...)
}

// NewVaListN returns a newly allocated va_list for n items. The caller of
// NewVaListN is responsible for freeing the va_list.
func NewVaListN(n int) (va_list uintptr) {
	return Xmalloc(nil, Tsize_t(8*n))
}

func SetEnviron(t *TLS, env []string) {
	__ccgo_environOnce.Do(func() {
		Xenviron = mustAllocStrings(env)
	})
}

func Dmesg(s string, args ...interface{}) {
	// nop
}

func Xalloca(tls *TLS, size Tsize_t) uintptr {
	return tls.alloca(size)
}

// struct cmsghdr *CMSG_NXTHDR(struct msghdr *msgh, struct cmsghdr *cmsg);
func X__cmsg_nxthdr(t *TLS, msgh, cmsg uintptr) uintptr {
	panic(todo(""))
}

func Cover() {
	runtime.Callers(2, coverPCs[:])
	Covered[coverPCs[0]] = struct{}{}
}

func CoverReport(w io.Writer) error {
	var a []string
	pcs := make([]uintptr, 1)
	for pc := range Covered {
		pcs[0] = pc
		frame, _ := runtime.CallersFrames(pcs).Next()
		a = append(a, fmt.Sprintf("%s:%07d:%s", filepath.Base(frame.File), frame.Line, frame.Func.Name()))
	}
	sort.Strings(a)
	_, err := fmt.Fprintf(w, "%s\n", strings.Join(a, "\n"))
	return err
}

func CoverC(s string) {
	CoveredC[s] = struct{}{}
}

func CoverCReport(w io.Writer) error {
	var a []string
	for k := range CoveredC {
		a = append(a, k)
	}
	sort.Strings(a)
	_, err := fmt.Fprintf(w, "%s\n", strings.Join(a, "\n"))
	return err
}

func X__ccgo_dmesg(t *TLS, fmt uintptr, va uintptr) {
	panic(todo(""))
}

func X__ccgo_getMutexType(tls *TLS, m uintptr) int32 { /* pthread_mutex_lock.c:3:5: */
	panic(todo(""))
}

func X__ccgo_in6addr_anyp(t *TLS) uintptr {
	panic(todo(""))
}

func X__ccgo_pthreadAttrGetDetachState(tls *TLS, a uintptr) int32 { /* pthread_attr_get.c:3:5: */
	panic(todo(""))
}

func X__ccgo_pthreadMutexattrGettype(tls *TLS, a uintptr) int32 { /* pthread_attr_get.c:93:5: */
	panic(todo(""))
}

// void sqlite3_log(int iErrCode, const char *zFormat, ...);
func X__ccgo_sqlite3_log(t *TLS, iErrCode int32, zFormat uintptr, args uintptr) {
	// nop
}

// unsigned __sync_add_and_fetch_uint32(*unsigned, unsigned)
func X__sync_add_and_fetch_uint32(t *TLS, p uintptr, v uint32) uint32 {
	return atomic.AddUint32((*uint32)(unsafe.Pointer(p)), v)
}

// unsigned __sync_sub_and_fetch_uint32(*unsigned, unsigned)
func X__sync_sub_and_fetch_uint32(t *TLS, p uintptr, v uint32) uint32 {
	return atomic.AddUint32((*uint32)(unsafe.Pointer(p)), -v)
}

var (
	randomData   = map[uintptr]*rand.Rand{}
	randomDataMu sync.Mutex
)

// The initstate_r() function is like initstate(3) except that it initializes
// the state in the object pointed to by buf, rather than initializing the
// global state  variable.   Before  calling this function, the buf.state field
// must be initialized to NULL.  The initstate_r() function records a pointer
// to the statebuf argument inside the structure pointed to by buf.  Thus,
// state‐ buf should not be deallocated so long as buf is still in use.  (So,
// statebuf should typically be allocated as a static variable, or allocated on
// the heap using malloc(3) or similar.)
//
// char *initstate_r(unsigned int seed, char *statebuf, size_t statelen, struct random_data *buf);
func Xinitstate_r(t *TLS, seed uint32, statebuf uintptr, statelen Tsize_t, buf uintptr) int32 {
	if buf == 0 {
		panic(todo(""))
	}

	randomDataMu.Lock()

	defer randomDataMu.Unlock()

	randomData[buf] = rand.New(rand.NewSource(int64(seed)))
	return 0
}

// int random_r(struct random_data *buf, int32_t *result);
func Xrandom_r(t *TLS, buf, result uintptr) int32 {
	randomDataMu.Lock()

	defer randomDataMu.Unlock()

	mr := randomData[buf]
	if RAND_MAX != math.MaxInt32 {
		panic(todo(""))
	}
	*(*int32)(unsafe.Pointer(result)) = mr.Int31()
	return 0
}

// void longjmp(jmp_buf env, int val);
func Xlongjmp(t *TLS, env uintptr, val int32) {
	panic(todo(""))
}

// void _longjmp(jmp_buf env, int val);
func X_longjmp(t *TLS, env uintptr, val int32) {
	panic(todo(""))
}

// int _obstack_begin (struct obstack *h, _OBSTACK_SIZE_T size, _OBSTACK_SIZE_T alignment,	void *(*chunkfun) (size_t),  void (*freefun) (void *))
func X_obstack_begin(t *TLS, obstack uintptr, size, alignment int32, chunkfun, freefun uintptr) int32 {
	panic(todo(""))
}

// extern void _obstack_newchunk(struct obstack *, int);
func X_obstack_newchunk(t *TLS, obstack uintptr, length int32) int32 {
	panic(todo(""))
}

// void obstack_free (struct obstack *h, void *obj)
func Xobstack_free(t *TLS, obstack, obj uintptr) {
	panic(todo(""))
}

// int obstack_vprintf (struct obstack *obstack, const char *template, va_list ap)
func Xobstack_vprintf(t *TLS, obstack, template, va uintptr) int32 {
	panic(todo(""))
}

// int _setjmp(jmp_buf env);
func X_setjmp(t *TLS, env uintptr) int32 {
	return 0 //TODO
}

// int setjmp(jmp_buf env);
func Xsetjmp(t *TLS, env uintptr) int32 {
	panic(todo(""))
}

// int backtrace(void **buffer, int size);
func Xbacktrace(t *TLS, buf uintptr, size int32) int32 {
	panic(todo(""))
}

// void backtrace_symbols_fd(void *const *buffer, int size, int fd);
func Xbacktrace_symbols_fd(t *TLS, buffer uintptr, size, fd int32) {
	panic(todo(""))
}

// int fts_close(FTS *ftsp);
func Xfts_close(t *TLS, ftsp uintptr) int32 {
	panic(todo(""))
}

// FTS *fts_open(char * const *path_argv, int options, int (*compar)(const FTSENT **, const FTSENT **));
func Xfts_open(t *TLS, path_argv uintptr, options int32, compar uintptr) uintptr {
	panic(todo(""))
}

// FTSENT *fts_read(FTS *ftsp);
func Xfts64_read(t *TLS, ftsp uintptr) uintptr {
	panic(todo(""))
}

// int fts_close(FTS *ftsp);
func Xfts64_close(t *TLS, ftsp uintptr) int32 {
	panic(todo(""))
}

// FTS *fts_open(char * const *path_argv, int options, int (*compar)(const FTSENT **, const FTSENT **));
func Xfts64_open(t *TLS, path_argv uintptr, options int32, compar uintptr) uintptr {
	panic(todo(""))
}

// FTSENT *fts_read(FTS *ftsp);
func Xfts_read(t *TLS, ftsp uintptr) uintptr {
	panic(todo(""))
}

// FILE *popen(const char *command, const char *type);
func Xpopen(t *TLS, command, type1 uintptr) uintptr {
	panic(todo(""))
}

// int sysctlbyname(const char *name, void *oldp, size_t *oldlenp, void *newp, size_t newlen);
func Xsysctlbyname(t *TLS, name, oldp, oldlenp, newp uintptr, newlen Tsize_t) int32 {
	oldlen := *(*Tsize_t)(unsafe.Pointer(oldlenp))
	switch GoString(name) {
	case "hw.ncpu":
		if oldlen != 4 {
			panic(todo(""))
		}

		*(*int32)(unsafe.Pointer(oldp)) = int32(runtime.GOMAXPROCS(-1))
		return 0
	default:
		panic(todo(""))
		t.setErrno(ENOENT)
		return -1
	}
}

// void uuid_copy(uuid_t dst, uuid_t src);
func Xuuid_copy(t *TLS, dst, src uintptr) {
	if __ccgo_strace {
		trc("t=%v src=%v, (%v:)", t, src, origin(2))
	}
	*(*uuid.Uuid_t)(unsafe.Pointer(dst)) = *(*uuid.Uuid_t)(unsafe.Pointer(src))
}

// int uuid_parse( char *in, uuid_t uu);
func Xuuid_parse(t *TLS, in uintptr, uu uintptr) int32 {
	if __ccgo_strace {
		trc("t=%v in=%v uu=%v, (%v:)", t, in, uu, origin(2))
	}
	r, err := guuid.Parse(GoString(in))
	if err != nil {
		return -1
	}

	copy((*RawMem)(unsafe.Pointer(uu))[:unsafe.Sizeof(uuid.Uuid_t{})], r[:])
	return 0
}

// void uuid_generate_random(uuid_t out);
func Xuuid_generate_random(t *TLS, out uintptr) {
	if __ccgo_strace {
		trc("t=%v out=%v, (%v:)", t, out, origin(2))
	}
	x := guuid.New()
	copy((*RawMem)(unsafe.Pointer(out))[:], x[:])
}

// void uuid_unparse(uuid_t uu, char *out);
func Xuuid_unparse(t *TLS, uu, out uintptr) {
	if __ccgo_strace {
		trc("t=%v out=%v, (%v:)", t, out, origin(2))
	}
	s := (*guuid.UUID)(unsafe.Pointer(uu)).String()
	copy((*RawMem)(unsafe.Pointer(out))[:], s)
	*(*byte)(unsafe.Pointer(out + uintptr(len(s)))) = 0
}

var Xzero_struct_address Taddress
