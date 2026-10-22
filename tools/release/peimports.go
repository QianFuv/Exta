package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"io"
	"sort"
	"strings"
)

// peImports reads DLL descriptors directly, including libraries imported only by ordinal.
// Go's debug/pe.ImportedLibraries is not implemented in the pinned toolchain.
func peImports(file *pe.File) ([]string, error) {
	header, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || header.NumberOfRvaAndSizes <= 13 || header.DataDirectory[13].VirtualAddress != 0 {
		return nil, errors.New("unsupported PE optional header or delay imports")
	}
	readRVA := func(address uint32) ([]byte, error) {
		for _, section := range file.Sections {
			if uint64(address) >= uint64(section.VirtualAddress) && uint64(address) < uint64(section.VirtualAddress)+uint64(section.Size) {
				data, err := io.ReadAll(io.NewSectionReader(section, 0, int64(section.Size)))
				offset := uint64(address) - uint64(section.VirtualAddress)
				if err != nil || offset >= uint64(len(data)) {
					return nil, errors.New("invalid PE import address")
				}
				return data[offset:], nil
			}
		}
		return nil, errors.New("PE import address is outside file sections")
	}
	directory := header.DataDirectory[1]
	if directory.VirtualAddress == 0 || directory.Size < 20 {
		return nil, errors.New("Windows candidate has no inspectable import table")
	}
	table, err := readRVA(directory.VirtualAddress)
	if err != nil || uint64(directory.Size) > uint64(len(table)) {
		return nil, errors.New("invalid PE import table bounds")
	}
	table = table[:directory.Size]
	libraries := map[string]bool{}
	for len(table) >= 20 {
		descriptor := table[:20]
		table = table[20:]
		if bytes.Equal(descriptor, make([]byte, 20)) {
			if len(libraries) == 0 {
				return nil, errors.New("empty PE import table")
			}
			result := make([]string, 0, len(libraries))
			for name := range libraries {
				result = append(result, name)
			}
			sort.Strings(result)
			return result, nil
		}
		name, err := readRVA(binary.LittleEndian.Uint32(descriptor[12:16]))
		if err != nil {
			return nil, err
		}
		end := bytes.IndexByte(name, 0)
		if end < 1 || end > 255 {
			return nil, errors.New("invalid PE import name")
		}
		for _, value := range name[:end] {
			if value < 33 || value > 126 || strings.ContainsRune(`/\:`, rune(value)) {
				return nil, errors.New("unsafe PE import name")
			}
		}
		libraries[strings.ToLower(string(name[:end]))] = true
	}
	return nil, errors.New("unterminated PE import descriptors")
}
