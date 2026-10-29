// Package deploy 内嵌部署相关文件，供主控对外提供。
package deploy

import _ "embed"

// InstallScript 为被控端一键安装脚本。
//
//go:embed install.sh
var InstallScript []byte
