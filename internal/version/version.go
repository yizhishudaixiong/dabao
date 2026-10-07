// Package version 存放程序内部版本号，是全项目唯一的版本来源。
// 界面显示、构建脚本（build.ps1）同步 info.json 都从这里读取。
package version

// Version 程序内部版本号。
// 修改版本号只需改这一处，然后运行 build.ps1 重新构建即可。
const Version = "6.0.0"
