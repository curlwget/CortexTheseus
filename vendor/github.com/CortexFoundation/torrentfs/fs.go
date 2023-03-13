// Copyright 2020 The CortexTheseus Authors
// This file is part of the CortexTheseus library.
//
// The CortexTheseus library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The CortexTheseus library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the CortexTheseus library. If not, see <http://www.gnu.org/licenses/>

package torrentfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CortexFoundation/CortexTheseus/common"
	"github.com/CortexFoundation/CortexTheseus/common/mclock"
	"github.com/CortexFoundation/CortexTheseus/log"
	"github.com/CortexFoundation/CortexTheseus/p2p"
	"github.com/CortexFoundation/CortexTheseus/p2p/dnsdisc"
	"github.com/CortexFoundation/CortexTheseus/p2p/enode"
	params1 "github.com/CortexFoundation/CortexTheseus/params"
	"github.com/CortexFoundation/CortexTheseus/rpc"
	"github.com/CortexFoundation/torrentfs/backend"
	"github.com/CortexFoundation/torrentfs/monitor"
	"github.com/CortexFoundation/torrentfs/params"
	"github.com/CortexFoundation/torrentfs/types"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	mapset "github.com/deckarep/golang-set/v2"
	//lru "github.com/hashicorp/golang-lru"
	cp "github.com/otiai10/copy"

	"github.com/ucwong/go-ttlmap"
)

const (
	SEED_SIG uint64 = 0
)

// TorrentFS contains the torrent file system internals.
type TorrentFS struct {
	protocol p2p.Protocol // Protocol description and parameters
	config   *params.Config
	monitor  *monitor.Monitor
	handler  *backend.TorrentManager
	db       *backend.ChainDB

	peerMu sync.RWMutex     // Mutex to sync the active peer set
	peers  map[string]*Peer // Set of currently active peers

	//queryChan chan Query

	//nasCache *lru.Cache
	//queryCache *lru.Cache
	nasCounter uint64

	received uint64
	sent     uint64

	in  uint64
	out uint64

	// global file hash & score
	//scoreTable map[string]int

	//seedingNotify chan string
	closeAll chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
	worm     mapset.Set[string]

	tunnel *ttlmap.Map

	callback chan any
	ttlchan  chan any

	net *p2p.Server
}

func (t *TorrentFS) storage() *backend.TorrentManager {
	return t.handler
}

func (t *TorrentFS) chain() *backend.ChainDB {
	return t.db
}

var (
	inst *TorrentFS = nil
	mut  sync.RWMutex
)

func GetStorage() CortexStorage {
	mut.RLock()
	defer mut.RUnlock()

	if inst == nil {
		//inst, _ = New(&DefaultConfig, true, false, false)
		log.Warn("Storage instance get failed, should new it first")
	}
	return inst //GetTorrentInstance()
}

// New creates a new torrentfs instance with the given configuration.
func New(config *params.Config, cache, compress, listen bool) (*TorrentFS, error) {
	mut.Lock()
	defer mut.Unlock()
	if inst != nil {
		log.Warn("Storage has been already inited", "storage", inst, "config", config, "cache", cache, "compress", compress, "listen", listen)
		return inst, nil
	}

	db, err := backend.NewChainDB(config)
	if err != nil {
		log.Error("file storage failed", "err", err)
		return nil, err
	}
	log.Info("File storage initialized")

	handler, err := backend.NewTorrentManager(config, db.ID(), cache, compress)
	if err != nil || handler == nil {
		log.Error("fs manager failed", "err", err)
		return nil, errors.New("fs download manager initialise failed")
	}
	log.Info("Fs manager initialized")

	_callback := make(chan any, 1)
	monitor, err := monitor.New(config, cache, compress, listen, db, _callback)
	if err != nil {
		log.Error("Failed create monitor", "err", err)
		return nil, err
	}

	log.Info("Fs monitor initialized")

	inst = &TorrentFS{
		config:  config,
		monitor: monitor,
		handler: handler,
		db:      db,
		peers:   make(map[string]*Peer),
		//queryChan: make(chan Query, 128),
	}

	//inst.nasCache, _ = lru.New(32)
	inst.callback = _callback
	inst.ttlchan = make(chan any, 1024)
	//inst.queryCache, _ = lru.New(25)

	//inst.scoreTable = make(map[string]int)
	//inst.seedingNotify = make(chan string, 32)

	inst.worm = mapset.NewSet[string]()

	inst.protocol = p2p.Protocol{
		Name:    params.ProtocolName,
		Version: uint(params.ProtocolVersion),
		Length:  params.NumberOfMessageCodes,
		Run:     inst.HandlePeer,
		NodeInfo: func() any {
			return map[string]any{
				"version": params.ProtocolVersion,
				"status": map[string]any{
					"dht":            !config.DisableDHT,
					"tcp":            !config.DisableTCP,
					"utp":            !config.DisableUTP,
					"port":           inst.LocalPort(),
					"root":           inst.chain().Root().Hex(),
					"files":          inst.Congress(),
					"active":         inst.Candidate(),
					"nominee":        inst.Nominee(),
					"leafs":          len(inst.chain().Blocks()),
					"number":         monitor.CurrentNumber(),
					"maxMessageSize": inst.MaxMessageSize(),
					//					"listen":         monitor.listen,
					"metrics":    inst.NasCounter(),
					"neighbours": inst.Neighbors(),
					"received":   inst.received,
					"sent":       inst.sent,
				},
				//"score": inst.scoreTable,
				"worm": inst.worm,
			}
		},
		PeerInfo: func(id enode.ID) any {
			inst.peerMu.RLock()
			defer inst.peerMu.RUnlock()
			if p := inst.peers[fmt.Sprintf("%x", id[:8])]; p != nil {
				if p.Info() != nil {
					return map[string]any{
						"version": p.version,
						"listen":  p.Info().Listen,
						"root":    p.Info().Root.Hex(),
						"files":   p.Info().Files,
						"leafs":   p.Info().Leafs,
					}
				} else {

					return map[string]any{
						"version": p.version,
					}
				}
			}
			return nil
		},
	}

	//add dns discovery
	if inst.protocol.DialCandidates == nil {
		log.Info("Nas dial candidates", "version", params.ProtocolVersion)
		client := dnsdisc.NewClient(dnsdisc.Config{})
		s, err := client.NewIterator([]string{params1.KnownDNSNetwork(params1.MainnetGenesisHash, "all")}...)
		if err == nil {
			inst.protocol.DialCandidates = s
		} else {
			log.Warn("Protocol dial candidates failed", "err", err)
		}
	}

	options := &ttlmap.Options{
		InitialCapacity: 1024,
		OnWillExpire: func(key string, item ttlmap.Item) {
			log.Debug("Nas msg expired", "ih", key, "v", item.Value())
		},
		OnWillEvict: func(key string, item ttlmap.Item) {
			log.Debug("Nas msg evicted", "ih", key, "v", item.Value())
		},
	}

	inst.tunnel = ttlmap.New(options)

	inst.closeAll = make(chan struct{})

	inst.wg.Add(1)
	go inst.listen()
	//inst.wg.Add(1)
	//go inst.process()
	//inst.init()

	log.Info("Fs instance created")

	return inst, nil
}

/*func (tfs *TorrentFS) process() {
	defer tfs.wg.Done()
	for {
		select {
		case k := <-tfs.seedingNotify:
			tfs.query(k, 0)
		case <-tfs.closeAll:
			return
		}
	}
}*/

func (fs *TorrentFS) listen() {
	log.Info("Bitsflow listener starting ...")
	defer fs.wg.Done()
	ttl := time.NewTimer(3 * time.Second)
	ticker := time.NewTicker(60 * time.Second)
	defer ttl.Stop()
	defer ticker.Stop()
	for {
		select {
		case msg := <-fs.callback:
			meta := msg.(*types.BitsFlow)
			//if meta.Request() > 0 || (params.IsGood(meta.InfoHash()) && fs.config.Mode != params.LAZY) {
			if meta.Request() > 0 || params.IsGood(meta.InfoHash()) {
				fs.download(context.Background(), meta.InfoHash(), meta.Request())
			} else {
				fs.ttlchan <- msg
			}
		case <-ttl.C:
			if len(fs.ttlchan) > 0 {
				msg := <-fs.ttlchan
				meta := msg.(*types.BitsFlow)
				fs.download(context.Background(), meta.InfoHash(), meta.Request())
			}
			ttl.Reset(3 * time.Second)
		case <-ticker.C:
			log.Info("Bitsflow status", "neighbors", fs.Neighbors(), "current", fs.monitor.CurrentNumber(), "rev", fs.received, "sent", fs.sent, "in", fs.in, "out", fs.out, "nocola", fs.out+uint64(fs.Neighbors())-fs.in)
		case <-fs.closeAll:
			log.Info("Bitsflow listener stop")
			return
		}
	}
}

func (fs *TorrentFS) MaxMessageSize() uint32 {
	return params.DefaultMaxMessageSize
}

/*func (fs *TorrentFS) find(ih string) (*Peer, error) {
	for s, p := range fs.peers {
		if p.seeding.Contains(ih) {
			// TODO
			log.Debug("Seed found !!!", "from", s, "ih", ih)
			return p, nil
		}
	}

	log.Debug("Seed not found !!!", "neighbors", len(fs.peers), "ih", ih)
	return nil, nil
}*/

func (fs *TorrentFS) HandlePeer(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
	//tfsPeer := newPeer(fmt.Sprintf("%x", peer.ID().Bytes()[:8]), fs, peer, rw)
	tfsPeer := newPeer(peer.ID().String(), fs, peer, rw)

	fs.peerMu.Lock()
	fs.peers[tfsPeer.id] = tfsPeer
	fs.in++
	fs.peerMu.Unlock()

	defer func() {
		fs.peerMu.Lock()
		delete(fs.peers, tfsPeer.id)
		fs.out++
		fs.peerMu.Unlock()
	}()

	if err := tfsPeer.handshake(); err != nil {
		return err
	}

	tfsPeer.start()
	defer func() {
		tfsPeer.stop()
	}()

	return fs.runMessageLoop(tfsPeer, rw)
}
func (fs *TorrentFS) runMessageLoop(p *Peer, rw p2p.MsgReadWriter) error {
	for {
		// fetch the next packet
		packet, err := rw.ReadMsg()
		if err != nil {
			log.Debug("message loop", "peer", p.peer.ID(), "err", err)
			return err
		}

		if packet.Size > fs.MaxMessageSize() {
			log.Warn("oversized message received", "peer", p.peer.ID())
			return errors.New("oversized message received")
		}

		log.Debug("Nas "+params.ProtocolVersionStr+" package", "size", packet.Size, "code", packet.Code)
		//fs.received++

		switch packet.Code {
		case params.StatusCode:
			var info *PeerInfo
			if err := packet.Decode(&info); err != nil {
				log.Warn("failed to decode peer state, peer will be disconnected", "peer", p.peer.ID(), "err", err)
				return errors.New("invalid peer state")
			}
			p.peerInfo = info
		case params.QueryCode:
			if params.ProtocolVersion >= 4 {
				var info *Query
				if err := packet.Decode(&info); err != nil {
					log.Warn("failed to decode msg, peer will be disconnected", "peer", p.peer.ID(), "err", err)
					return errors.New("invalid msg")
				}

				if !common.IsHexAddress(info.Hash) {
					return errors.New("invalid address")
				}

				//if info.Size > 0 {
				//fs.wakeup(context.Background(), info.Hash, info.Size)
				fs.wakeup(context.Background(), info.Hash)
				//}
			}
		case params.MsgCode:
			if params.ProtocolVersion > 4 {
				var info *MsgInfo
				if err := packet.Decode(&info); err != nil {
					log.Warn("failed to decode msg, peer will be disconnected", "peer", p.peer.ID(), "err", err)
					return errors.New("invalid msg")
				}
				log.Warn("Nas msg testing", "code", params.MsgCode, "desc", info.Desc)
			}
		default:
			log.Warn("Encounter package code", "code", packet.Code)
			return errors.New("invalid code")
		}
		packet.Discard()
	}
}

func (fs *TorrentFS) progress(ih string) (uint64, error) {
	return fs.chain().GetTorrentProgress(ih)
}

// Protocols implements the node.Service interface.
func (fs *TorrentFS) Protocols() []p2p.Protocol { return []p2p.Protocol{fs.protocol} }

// APIs implements the node.Service interface.
func (fs *TorrentFS) APIs() []rpc.API {
	return []rpc.API{
		{
			Namespace: params.ProtocolName,
			Version:   params.ProtocolVersionStr,
			Service:   NewPublicTorrentAPI(fs),
			Public:    false,
		},
	}
}

func (fs *TorrentFS) Version() uint {
	return fs.protocol.Version
}

// Start starts the data collection thread and the listening server of the dashboard.
// Implements the node.Service interface.
func (tfs *TorrentFS) Start(srvr *p2p.Server) (err error) {
	log.Info("Fs server starting ... ...")
	if tfs == nil || tfs.monitor == nil {
		log.Warn("Storage fs init failed", "fs", tfs)
		return
	}

	// Figure out a max peers count based on the server limits
	if srvr != nil {
		log.Info("P2p net bounded")
		tfs.net = srvr
	}

	log.Info("Started nas", "config", tfs, "mode", tfs.config.Mode, "version", params.ProtocolVersion, "queue", tfs.tunnel.Len(), "peers", tfs.Neighbors())

	err = tfs.handler.Start()
	if err != nil {
		return
	}

	err = tfs.monitor.Start()

	tfs.init()

	return
}

func (fs *TorrentFS) init() {
	if fs.config.Mode != params.LAZY {
		checkpoint := fs.chain().GetRoot(395964)
		log.Info("Checkpoint loaded")
		if checkpoint == nil {
			for k, ok := range params.ColaFiles {
				if ok {
					fs.bitsflow(context.Background(), k, 1024*1024*1024)
				}
			}
		}
	}

	log.Info("Init finished")
}

// download and pub
func (fs *TorrentFS) bitsflow(ctx context.Context, ih string, size uint64) error {
	select {
	case fs.callback <- types.NewBitsFlow(ih, size):
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// Stop stops the data collection thread and the connection listener of the dashboard.
// Implements the node.Service interface.
func (tfs *TorrentFS) Stop() error {
	if tfs == nil {
		log.Info("Cortex fs engine is already stopped")
		return errors.New("fs has been stopped")
	}

	tfs.once.Do(func() {
		log.Info("Fs client listener synchronizing closing")
		if tfs.handler != nil {
			tfs.handler.Close()
		}
		if tfs.db != nil {
			tfs.db.Close()
		}
		if tfs.monitor != nil {
			tfs.monitor.Stop()
		}
		close(tfs.closeAll)
	})

	tfs.wg.Wait()

	if tfs.tunnel != nil {
		tfs.tunnel.Drain()
	}
	log.Info("Cortex fs engine stopped")
	return nil
}

func (fs *TorrentFS) broadcast(ih string, rawSize uint64) bool {
	if !common.IsHexAddress(ih) {
		return false
	}

	if s, err := fs.tunnel.Get(ih); err == nil && s.Value().(uint64) >= rawSize {
		return false
	}

	fs.tunnel.Set(ih, ttlmap.NewItem(rawSize, ttlmap.WithTTL(60*time.Second)), nil)

	return true
}

/*func (fs *TorrentFS) notify(infohash string) bool {
	if !common.IsHexAddress(infohash) {
		return false
	}

	fs.nasCache.Add(infohash, SEED_SIG)

	return true
}*/

func (fs *TorrentFS) IsActive(err error) bool {
	return !errors.Is(err, backend.ErrInactiveTorrent)
}

// Available is used to check the file status
// func (fs *TorrentFS) wakeup(ctx context.Context, ih string, rawSize uint64) { //(bool, error) {
func (fs *TorrentFS) wakeup(ctx context.Context, ih string) {
	//	exist, _, _, err := fs.storage().Exists(ih, rawSize)
	//	if !fs.IsActive(err) {
	if p, e := fs.progress(ih); e == nil {
		//fs.bitsflow(ctx, ih, p) // to be downloaded
		//if err := fs.storage().Search(ctx, ih, p); err != nil {
		//return err
		//	log.Warn("Wake up failed", "ih", ih)
		//}
		fs.storage().Search(ctx, ih, p)
	}
	//	}
	//
	// return exist, err
}

func (fs *TorrentFS) GetFileWithSize(ctx context.Context, infohash string, rawSize uint64, subpath string) ([]byte, error) {
	log.Debug("Get file with size", "ih", infohash, "size", common.StorageSize(rawSize), "path", subpath)
	if ret, err := fs.storage().GetFile(ctx, infohash, subpath); err != nil {
		fs.wg.Add(1)
		go func(ctx context.Context, ih string) {
			defer fs.wg.Done()
			//fs.wakeup(ctx, infohash, rawSize)
			fs.wakeup(ctx, ih)
		}(ctx, infohash)
		//if fs.config.Mode == params.LAZY && params.IsGood(infohash) {
		if params.IsGood(infohash) {
			start := mclock.Now()
			log.Info("Downloading ... ...", "ih", infohash, "size", common.StorageSize(rawSize), "neighbors", fs.Neighbors(), "current", fs.monitor.CurrentNumber())
			t := time.NewTimer(500 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					if ret, err := fs.storage().GetFile(ctx, infohash, subpath); err != nil {
						log.Debug("File downloading ... ...", "ih", infohash, "size", common.StorageSize(rawSize), "path", subpath, "err", err)
						t.Reset(100 * time.Millisecond)
					} else {
						elapsed := time.Duration(mclock.Now()) - time.Duration(start)
						log.Info("Downloaded", "ih", infohash, "size", common.StorageSize(rawSize), "neighbors", fs.Neighbors(), "elapsed", common.PrettyDuration(elapsed), "current", fs.monitor.CurrentNumber())
						if uint64(len(ret)) > rawSize {
							return nil, backend.ErrInvalidRawSize
						}
						return ret, err
					}
				case <-ctx.Done():
					log.Warn("Timeout", "ih", infohash, "size", common.StorageSize(rawSize), "err", ctx.Err())
					return nil, ctx.Err()
				}
			}
		}
		return nil, err
	} else {
		if uint64(len(ret)) > rawSize {
			return nil, backend.ErrInvalidRawSize
		}
		log.Debug("Get File directly", "ih", infohash, "size", common.StorageSize(rawSize), "path", subpath, "ret", len(ret))
		if !params.IsGood(infohash) {
			go fs.encounter(infohash)
		}
		return ret, nil
	}
}

func (fs *TorrentFS) encounter(ih string) {
	if !fs.worm.Contains(ih) {
		fs.worm.Add(ih)
	}
}

// Seeding Local File, validate folder, seeding and
// load files, default mode is copyMode, linkMode
// will limit user's operations for original files
func (fs *TorrentFS) SeedingLocal(ctx context.Context, filePath string, isLinkMode bool) (infoHash string, err error) {
	// 1. check folder exist
	if _, err = os.Stat(filePath); err != nil {
		return
	}

	// 2. check subfile data exist and not empty:
	// recursively iterate until meet file not empty
	var iterateForValidFile func(basePath string, dataInfo os.FileInfo) bool
	iterateForValidFile = func(basePath string, dataInfo os.FileInfo) bool {
		filePath := filepath.Join(basePath, dataInfo.Name())
		if dataInfo.IsDir() {
			dirFp, _ := os.Open(filePath)
			if fInfos, err := dirFp.Readdir(0); err != nil {
				log.Error("Read dir failed", "filePath", filePath, "err", err)
				return false
			} else {
				for _, v := range fInfos {
					// return as soon as possible if meet 'true', else continue
					if iterateForValidFile(filePath, v) {
						return true
					}
				}
			}
		} else if dataInfo.Size() > 0 {
			return true
		}
		return false
	}

	var (
		dataInfo os.FileInfo
		fileMode bool = false
	)
	dataPath := filepath.Join(filePath, "data")
	if dataInfo, err = os.Stat(dataPath); err != nil {
		dataPath = filepath.Join(filePath, "")
		if dataInfo, err = os.Stat(dataPath); err != nil {
			log.Error("Load data failed", "dataPath", dataPath)
			return
		}
		fileMode = true
	}
	validFlag := iterateForValidFile(filePath, dataInfo)
	if !validFlag {
		err = errors.New("SeedingLocal: Empty Seeding Data!")
		log.Error("SeedingLocal", "check", err.Error(), "path", dataPath, "name", dataInfo.Name(), "fileMode", fileMode)
		return
	}

	// 3. generate torrent file, rewrite if exists
	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{params.MainnetTrackers},
	}
	mi.SetDefaults()
	info := metainfo.Info{PieceLength: 256 * 1024}
	if err = info.BuildFromFilePath(dataPath); err != nil {
		return
	}
	if mi.InfoBytes, err = bencode.Marshal(info); err != nil {
		return
	}

	torrentPath := filepath.Join(filePath, "torrent")
	if fileMode {
		torrentPath = filepath.Join("", "torrent")
	}

	var fileTorrent *os.File
	fileTorrent, err = os.OpenFile(torrentPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	if err = mi.Write(fileTorrent); err != nil {
		return
	}

	// 4. copy or link, will not cover if dst exist!
	ih := common.Address(mi.HashInfoBytes())
	log.Info("Local file Seeding", "ih", ih.Hex(), "path", dataPath)
	infoHash = strings.TrimPrefix(strings.ToLower(ih.Hex()), common.Prefix)
	linkDst := filepath.Join(fs.storage().TmpDataDir, infoHash)
	if !isLinkMode {
		if !fileMode {
			err = cp.Copy(filePath, linkDst)
		} else {
			err = os.MkdirAll(filepath.Dir(linkDst), 0777) //os.FileMode(os.ModePerm))
			if err != nil {
				log.Error("Mkdir failed", "path", linkDst)
				return
			}

			err = cp.Copy(filePath, filepath.Join(linkDst, dataInfo.Name()))
			if err != nil {
				log.Error("Mkdir failed", "filePath", filePath, "path", linkDst)
				return
			}
			log.Info("Torrent copy", "torrentPath", torrentPath, "linkDst", linkDst)
			err = cp.Copy(torrentPath, filepath.Join(linkDst, "torrent"))
			if err != nil {
				log.Error("Mkdir failed", "torrentPath", torrentPath, "path", linkDst)
				return
			}

		}
	} else {

		if fileMode {
			//TODO
			return
		}
		// check if symbol link exist
		if _, err = os.Stat(linkDst); err == nil {
			// choice-1: original symbol link exists, cover it. (passed)

			// choice-2: original symbol link exists, return error
			err = os.ErrExist
		} else {
			// create symbol link
			var absOriFilePath string
			if absOriFilePath, err = filepath.Abs(filePath); err == nil {
				err = os.Symlink(absOriFilePath, linkDst)
			}
		}
	}

	// 5. seeding
	if err == nil || errors.Is(err, os.ErrExist) {
		log.Debug("SeedingLocal", "dest", linkDst, "err", err)
		err = fs.storage().Search(context.Background(), ih.Hex(), 0)
		if err == nil {
			fs.storage().AddLocalSeedFile(infoHash)
		}
	}

	return
}

// PauseSeeding Local File
func (fs *TorrentFS) PauseLocalSeed(ctx context.Context, ih string) error {
	return fs.storage().PauseLocalSeedFile(ih)
}

// ResumeSeeding Local File
func (fs *TorrentFS) ResumeLocalSeed(ctx context.Context, ih string) error {
	return fs.storage().ResumeLocalSeedFile(ih)
}

// List All Torrents Status (read-only)
func (fs *TorrentFS) ListAllTorrents(ctx context.Context) map[string]map[string]int {
	return fs.storage().ListAllTorrents()
}

func (fs *TorrentFS) Tunnel(ctx context.Context, ih string) error {
	if err := fs.storage().Search(ctx, ih, 1024*1024*1024); err != nil {
		return err
	}
	return nil
}

func (fs *TorrentFS) Drop(ih string) error {
	if err := fs.storage().Drop(ih); err != nil {
		return err
	}
	return nil
}

// Download is used to download file with request, broadcast when not found locally
func (fs *TorrentFS) download(ctx context.Context, ih string, request uint64) error {
	ih = strings.ToLower(ih)
	_, p, err := fs.chain().SetTorrentProgress(ih, request)
	if err != nil {
		return err
	}
	if exist, _, _, _ := fs.storage().Exists(ih, request); !exist {
		fs.wg.Add(1)
		go func(ih string, p uint64) {
			defer fs.wg.Done()
			s := fs.broadcast(ih, p)
			if s {
				log.Debug("Nas "+params.ProtocolVersionStr+" tunnel", "ih", ih, "request", common.StorageSize(float64(p)), "queue", fs.tunnel.Len(), "peers", fs.Neighbors())
			}
		}(ih, p)
	}
	// local search
	if err := fs.storage().Search(ctx, ih, p); err != nil {
		return err
	}

	return nil
}

func (fs *TorrentFS) Download(ctx context.Context, ih string, request uint64) error {
	return fs.bitsflow(ctx, ih, request)
	//return fs.download(ctx, ih, request)
}

func (fs *TorrentFS) Status(ctx context.Context, ih string) (int, error) {
	if fs.storage().IsPending(ih) {
		return 1, nil
	}

	if fs.storage().IsDownloading(ih) {
		return 2, nil
	}

	if fs.storage().IsSeeding(ih) {
		return 0, nil
	}

	return 3, nil
}

func (fs *TorrentFS) LocalPort() int {
	return fs.storage().LocalPort()
}

//func (fs *TorrentFS) Simulate() {
//	fs.storage().Simulate()
//}

func (fs *TorrentFS) Congress() int {
	return fs.storage().Congress()
}

func (fs *TorrentFS) FullSeed() map[string]*backend.Torrent {
	return fs.storage().FullSeed()
}

func (fs *TorrentFS) Candidate() int {
	return fs.storage().Candidate()
}

func (fs *TorrentFS) NasCounter() uint64 {
	return fs.nasCounter
}

func (fs *TorrentFS) Nominee() int {
	return fs.storage().Nominee()
}

func (fs *TorrentFS) Envelopes() *ttlmap.Map {
	fs.peerMu.RLock()
	defer fs.peerMu.RUnlock()

	return fs.tunnel
}

func (fs *TorrentFS) Neighbors() int {
	if fs.net != nil {
		return fs.net.PeerCount()
	}

	return len(fs.peers)
}
