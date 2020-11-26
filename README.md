## Related projects
### CVM runtime (AI container)
https://github.com/CortexFoundation/cvm-runtime
### File storage
Stop your cortex full node daemon, when you do this test

https://github.com/CortexFoundation/torrentfs
```
git clone https://github.com/CortexFoundation/torrentfs.git
cd torrentfs
make && ./build/bin/torrent download 'ih:6b75cc1354495ec763a6b295ee407ea864a0c292'
downloaded ALL the torrents !!!!!!!!!!!!!!!!!!!

*** Make sure you can download the file successfully
*** Accept in/out traffic of fw settings as possible for stable and fast downloading speed
(40401 40404 5008 both in and out(tcp udp) traffic accepted at least)
```
### AI wrapper (Fixed API for inference and file storage)
https://github.com/CortexFoundation/inference
### PoW (Cortex Cuckoo cycle)
https://github.com/CortexFoundation/solution

## System Requirements
### ubuntu
Cortex node is developed in Ubuntu 18.04 x64 + CUDA 9.2 + NVIDIA Driver 396.37 environment, with CUDA Compute capability >= 6.1. Latest Ubuntu distributions are also compatible, but not fully tested.
Recommend:
- cmake 3.11.0+
 ```
wget https://cmake.org/files/v3.11/cmake-3.11.0-rc4-Linux-x86_64.tar.gz
tar zxvf cmake-3.11.0-rc4-Linux-x86_64.tar.gz
sudo mv cmake-3.11.0-rc4-Linux-x86_64  /opt/cmake-3.11
sudo ln -sf /opt/cmake-3.11/bin/*  /usr/bin/
 ```
- go 1.14.x
```
wget https://dl.google.com/go/go1.14.2.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.14.2.linux-amd64.tar.gz
echo 'export PATH="$PATH:/usr/local/go/bin"' >> ~/.bashrc
source ~/.bashrc
```
- gcc/g++ 5.4+
```
sudo apt install gcc
sudo apt install g++
```
- cuda 9.2+ (if u have gpu)
```
export LD_LIBRARY_PATH=/usr/local/cuda/lib64/:/usr/local/cuda/lib64/stubs:$LD_LIBRARY_PATH
export LIBRARY_PATH=/usr/local/cuda/lib64/:/usr/local/cuda/lib64/stubs:$LIBRARY_PATH
```
- nvidia driver 396.37+ reference: https://docs.nvidia.com/cuda/cuda-toolkit-release-notes/index.html#major-components
- ubuntu 18.04+
### centos
Recommend:
- cmake 3.11.0+
- go 1.14.x
- gcc/g++ 5.4+ reference: https://docs.nvidia.com/cuda/cuda-installation-guide-linux/index.html#system-requirements
- cuda 10.1+ (if u have gpu)
```
export LD_LIBRARY_PATH=/usr/local/cuda/lib64/:/usr/local/cuda/lib64/stubs:$LD_LIBRARY_PATH
export LIBRARY_PATH=/usr/local/cuda/lib64/:/usr/local/cuda/lib64/stubs:$LIBRARY_PATH
```
- nvidia driver 418.67+
- centos 7.6

## Cortex Full Node

### Compile Source Code
1. git clone --recursive https://github.com/CortexFoundation/CortexTheseus.git
2. cd CortexTheseus
3. make clean && make -j$(nproc)

(If failed, run ```rm -rf cvm-runtime && git submodule init && git submodule update``` and try again)

### Running Bash

And then, run any command to start full node `cortex`:

```Bash
1. cd CortexTheseus
2. export LD_LIBRARY_PATH=$PWD:$PWD/plugins:$LD_LIBRARY_PATH
3. ./build/bin/cortex

It is easy for you to view the help document by running ./build/bin/cortex --help
```
