#!/usr/bin/env bash
set -o errexit -o pipefail

readonly CI_FLAG=ci
readonly TEST_ACC_FLAG=testacc

RED='\033[0;31m'
GREEN='\033[0;32m'
INVERTED='\033[7m'
NC='\033[0m' # No Color

echo -e "${INVERTED}"
echo "USER: " + $USER
echo "PATH: " + $PATH
echo "GOPATH:" + $GOPATH
echo -e "${NC}"

##
# Tidy dependencies
##
go mod tidy
tidyResult=$?
if [ ${tidyResult} != 0 ]; then
	echo -e "${RED}✗ go mod tidy${NC}\n$tidyResult${NC}"
	exit 1
else echo -e "${GREEN}√ go mod tidy${NC}"
fi

##
# GO BUILD
# FIXME: Only linux and macos work, we lack RTLD_NODELETE and RTLD_NOLOAD in windows (we do noop to bypass the error but we still have an issue)
##
if [ "$1" == "$CI_FLAG" ] || [ "$2" == "$CI_FLAG" ]; then
	# build all binaries
	# We can't use Zig to compile for linux due to https://github.com/ziglang/zig/issues/21007
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -x -o bin/terraform-provider-nvkind-linux-amd64
	goBuildResult=$?
	if [ ${goBuildResult} != 0 ]; then
		echo -e "${RED}✗ go build (linux)${NC}\n$goBuildResult${NC}"
		exit 1
	else echo -e "${GREEN}√ go build (linux)${NC}"
	fi
	# We need macos SDK
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="zig cc -target x86_64-macos" CXX="zig c++ -target x86_64-macos" go build -x -o bin/terraform-provider-nvkind-darwin-amd64
	goBuildResult=$?
	if [ ${goBuildResult} != 0 ]; then
		echo -e "${RED}✗ go build (mac)${NC}\n$goBuildResult${NC}"
		exit 1
	else echo -e "${GREEN}√ go build (mac)${NC}"
	fi
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="zig cc -target x86_64-windows-gnu" CXX="zig c++ -target x86_64-windows-gnu" CGO_CFLAGS="-DRTLD_NODELETE=0 -DRTLD_NOLOAD=0" go build -x -o bin/terraform-provider-nvkind-windows-amd64
	goBuildResult=$?
	if [ ${goBuildResult} != 0 ]; then
		echo -e "${RED}✗ go build (windows)${NC}\n$goBuildResult${NC}"
		exit 1
	else echo -e "${GREEN}√ go build (windows)${NC}"
	fi
else
	# build just current arch
	CGO_ENABLED=1 go build -o bin/terraform-provider-nvkind
	goBuildResult=$?
	if [ ${goBuildResult} != 0 ]; then
		echo -e "${RED}✗ go build (dev)${NC}\n$goBuildResult${NC}"
		exit 1
	else echo -e "${GREEN}√ go build (dev)${NC}"
	fi
fi


##
# Verify dependencies
##
echo "? go mod verify"
depResult=$(go mod verify)
if [ $? != 0 ]; then
	echo -e "${RED}✗ go mod verify\n$depResult${NC}"
	exit 1
else echo -e "${GREEN}√ go mod verify${NC}"
fi

##
# GO TEST
##
echo "? go test"
go test ./...
# Check if tests passed
if [ $? != 0 ]; then
	echo -e "${RED}✗ go test\n${NC}"
	exit 1
else echo -e "${GREEN}√ go test${NC}"
fi

goFilesToCheck=$(find . -type f -name "*.go" | egrep -v "\/vendor\/|_*/automock/|_*/testdata/|_*export_test.go")

##
# TF ACCEPTANCE TESTS
##
if [ "$1" == "$TEST_ACC_FLAG" ] || [ "$2" == "$TEST_ACC_FLAG" ]; then
	# run terraform acceptance tests
	if [ "$1" == "$CI_FLAG" ] || [ "$2" == "$CI_FLAG" ]; then
		TF_ACC=1 go test ./nvkind -v -count 1 -parallel 20 -timeout 120m
	else 
		TF_ACC=1 go test ./nvkind -v -count 1 -parallel 1 -timeout 120m
	fi
fi

#
# GO FMT
#
goFmtResult=$(echo "${goFilesToCheck}" | xargs -L1 go fmt)
if [ $(echo ${#goFmtResult}) != 0 ]
	then
    	echo -e "${RED}✗ go fmt${NC}\n$goFmtResult${NC}"
    	exit 1;
	else echo -e "${GREEN}√ go fmt${NC}"
fi

##
# GO VET
##
packagesToVet=("./nvkind/...")

for vPackage in "${packagesToVet[@]}"; do
	vetResult=$(go vet ${vPackage})
	if [ $(echo ${#vetResult}) != 0 ]; then
		echo -e "${RED}✗ go vet ${vPackage} ${NC}\n$vetResult${NC}"
		exit 1
	else echo -e "${GREEN}√ go vet ${vPackage} ${NC}"
	fi
done
