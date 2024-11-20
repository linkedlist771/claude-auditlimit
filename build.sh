#!/bin/bash

# 在项目根目录下执行
go build

# 或者指定输出文件名
go build -o main
# 交叉编译到linux
GOOS=linux GOARCH=amd64 go build -o main

