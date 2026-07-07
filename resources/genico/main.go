// genico converts resources/icon.png into a Windows .ico file using the
// PNG-in-ICO format (the PNG bytes are wrapped in a minimal ICO container
// without decoding/re-encoding). Works on Windows Vista and later.
//
// Run: go run resources/genico/main.go
package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	pngData, err := os.ReadFile("resources/icon.png")
	if err != nil {
		fmt.Fprintf(os.Stderr, "genico: %v\n", err)
		os.Exit(1)
	}

	// ICO header (6 bytes) + one directory entry (16 bytes) = 22 bytes overhead.
	const headerSize = 6
	const entrySize = 16
	dataOffset := uint32(headerSize + entrySize)

	ico := make([]byte, 0, headerSize+entrySize+len(pngData))

	// ICONDIR
	ico = binary.LittleEndian.AppendUint16(ico, 0) // reserved
	ico = binary.LittleEndian.AppendUint16(ico, 1) // type = icon
	ico = binary.LittleEndian.AppendUint16(ico, 1) // count = 1 image

	// ICONDIRENTRY
	ico = append(ico, 0)                            // width (0 = 256+, actual size in PNG header)
	ico = append(ico, 0)                            // height (0 = 256+, actual size in PNG header)
	ico = append(ico, 0)                            // color count (0 = ≥256 colors)
	ico = append(ico, 0)                            // reserved
	ico = binary.LittleEndian.AppendUint16(ico, 1)  // planes
	ico = binary.LittleEndian.AppendUint16(ico, 32) // bit count
	ico = binary.LittleEndian.AppendUint32(ico, uint32(len(pngData)))
	ico = binary.LittleEndian.AppendUint32(ico, dataOffset)

	// PNG image data
	ico = append(ico, pngData...)

	if err := os.WriteFile("resources/icon.ico", ico, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "genico: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Generated resources/icon.ico")
}
