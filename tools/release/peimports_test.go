package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"runtime"
	"testing"
)

func TestPEImportsReadsNativeExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native PE evidence requires Windows")
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := pe.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	imports, err := peImports(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range imports {
		if name == "kernel32.dll" {
			return
		}
	}
	t.Fatalf("Windows runtime imports not observed: %v", imports)
}

func TestPEImportsIncludesOrdinalOnlyLibrariesAndRejectsMalformedTables(t *testing.T) {
	data := make([]byte, 128)
	binary.LittleEndian.PutUint32(data[12:16], 0x1040)
	binary.LittleEndian.PutUint32(data[16:20], 0x1060)
	copy(data[64:], "SQLite3.dll\x00")
	binary.LittleEndian.PutUint64(data[96:], 1<<63|1)
	header := &pe.OptionalHeader64{NumberOfRvaAndSizes: 16}
	header.DataDirectory[1] = pe.DataDirectory{VirtualAddress: 0x1000, Size: 40}
	file := &pe.File{OptionalHeader: header, Sections: []*pe.Section{{SectionHeader: pe.SectionHeader{VirtualAddress: 0x1000, Size: uint32(len(data))}, ReaderAt: bytes.NewReader(data)}}}
	imports, err := peImports(file)
	if err != nil || len(imports) != 1 || imports[0] != "sqlite3.dll" {
		t.Fatalf("ordinal import: %v %v", imports, err)
	}
	header.DataDirectory[1].Size = 20
	if _, err = peImports(file); err == nil {
		t.Fatal("accepted missing terminator")
	}
	header.DataDirectory[1].Size = 40
	binary.LittleEndian.PutUint32(data[12:16], 0xffffffff)
	if _, err = peImports(file); err == nil {
		t.Fatal("accepted unmapped library name")
	}
}
