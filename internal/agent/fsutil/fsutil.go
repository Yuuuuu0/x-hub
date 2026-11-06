// Package fsutil 提供被控端持久化文件的原子写入：先写同目录临时文件并 fsync，再 rename 替换，
// 避免断电或崩溃时留下截断的文件。
package fsutil

import (
	"os"
	"path/filepath"
)

// File 为一个待原子写入的文件。
type File struct {
	Path string
	Data []byte
	Perm os.FileMode
}

// WriteFile 原子写入单个文件，perm 为最终文件权限。
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return WriteFiles(File{Path: path, Data: data, Perm: perm})
}

// WriteFiles 先把所有文件写入各自目录下的临时文件并 fsync，全部成功后再按顺序 rename。
// 用于证书与私钥等需要成对更新的文件，缩小两者不配对的窗口；任一临时文件写入失败时不替换任何文件。
func WriteFiles(files ...File) error {
	tmps := make([]string, 0, len(files))
	defer func() {
		for _, t := range tmps {
			os.Remove(t) // rename 成功后临时文件已不存在，删除失败可忽略
		}
	}()
	for _, f := range files {
		tmp, err := writeTemp(f)
		if err != nil {
			return err
		}
		tmps = append(tmps, tmp)
	}
	for i, f := range files {
		if err := os.Rename(tmps[i], f.Path); err != nil {
			return err
		}
	}
	return nil
}

// writeTemp 在目标文件同目录创建临时文件，写入内容、设置权限并 fsync，返回临时文件路径。
func writeTemp(f File) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), "."+filepath.Base(f.Path)+".tmp-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_, err = tmp.Write(f.Data)
	if err == nil {
		err = tmp.Chmod(f.Perm)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}
