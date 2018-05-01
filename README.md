mpbuild
=======

Apache license.

### Features :

### Install :

- `brew install go`
- `go get -u -v github.com/aubonbeurre/mpbuild`

### Synopsis :

`~/go/bin/mpbuild -h`

### Example :

`mpbuild -q -l log.txt -c Debug -only U /sources/xxxapps/main/PXXX/Make/mpbuild.json`

### Settings :

`~/.mpbuild`:

- `workers`: number of concurrent xcodebuild
- `threads`: number of threads used by each xcodebuild

### Credits :

### See also :

https://github.com/aubonbeurre/homebrew-tap
https://github.com/aubonbeurre/homebrew-distcc/tree/myv3.2rc1.2
