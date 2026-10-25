package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseArchive(t *testing.T) {
	for _, target := range []string{"windows/amd64", "linux/amd64"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			binary := "exta-linux-amd64"
			if target == "windows/amd64" {
				binary = "exta-windows-amd64.exe"
			}
			for _, directory := range []string{"licenses", "frontend-licenses/font"} {
				if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
					t.Fatal(err)
				}
			}
			files := []string{binary, "RELEASE-NOTES.txt", "dependency-inventory.json", "frontend-provenance.json", "licenses/OFL.txt", "frontend-licenses/font/NOTICE.md"}
			for _, name := range files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			name := archiveName("0.1.5", target)
			expectedName := "exta_0.1.5_linux_amd64.tar.gz"
			if target == "windows/amd64" {
				expectedName = "exta_0.1.5_windows_amd64.zip"
			}
			if name != expectedName {
				t.Fatalf("archive name: %s", name)
			}
			if err := archive(root, binary, name); err != nil {
				t.Fatal(err)
			}
			contents := map[string]string{}
			modes := map[string]int64{}
			if target == "windows/amd64" {
				reader, err := zip.OpenReader(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				for _, entry := range reader.File {
					stream, err := entry.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(stream)
					stream.Close()
					if err != nil {
						t.Fatal(err)
					}
					contents[entry.Name] = string(data)
					modes[entry.Name] = int64(entry.Mode().Perm())
				}
			} else {
				file, err := os.Open(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				compressed, err := gzip.NewReader(file)
				if err != nil {
					t.Fatal(err)
				}
				defer compressed.Close()
				reader := tar.NewReader(compressed)
				for {
					entry, err := reader.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					if err != nil {
						t.Fatal(err)
					}
					contents[entry.Name] = string(data)
					modes[entry.Name] = entry.Mode
				}
			}
			if len(contents) != len(files) {
				t.Fatalf("archive membership: %v", contents)
			}
			for _, path := range files {
				mode := int64(0644)
				if path == binary {
					mode = 0755
				}
				if contents[path] != path || modes[path] != mode {
					t.Fatalf("entry %s: content=%q mode=%o", path, contents[path], modes[path])
				}
			}
			if err := archive(root, binary, name); err == nil {
				t.Fatal("existing archive was replaced")
			}
		})
	}
}
