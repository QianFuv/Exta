// Command embedpack writes one deterministic, synced archive for atomic UI promotion.
package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: embedpack <staged-directory> <new-archive>")
		os.Exit(1)
	}
	if err := pack(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pack(source, destination string) error {
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	err = filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errors.New("archive links forbidden")
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: filepath.ToSlash(relative), Method: zip.Deflate}
		header.SetMode(0644)
		output, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = output.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return file.Sync()
}
