// Package version 保存构建时通过 ldflags 注入的版本号。
package version

// Version 为当前程序的构建版本，发布构建时通过 -ldflags "-X .../version.Version=..." 注入。
// 主控为发布版本或提交短哈希；被控端为最后一次改动被控端相关代码的提交短哈希，只改主控或界面时不变。
var Version = "dev"

// AgentVersion 为主控分发的被控端版本，仅主控构建时注入，用于判断已接入的被控端是否需要更新。
var AgentVersion = "dev"
