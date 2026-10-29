// Package version 保存构建时通过 ldflags 注入的版本号。
package version

// Version 为当前构建版本，发布构建时通过 -ldflags "-X .../version.Version=vX.Y.Z" 注入。
var Version = "dev"
