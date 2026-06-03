package internals	

import (
	"fmt"
	"os"
	"io"
	"encoding/binary"
)

const (
	DIM				= 14
	NUM_CLUSTERS	= 1024
	STRIDE			= 16    // padded storage width
)

type RefData struct {
	centroids	[]int8		// NUM_CLUSTERS * STRIDE
	offsets		[]uint32	// NUMCLUSTERS + 1
	vectors		[]int8		// count * STRIDE, in cluster order
	labels		[]uint8		// count, in cluster order
	count		int
}

// -1 -> -128, [0,1] -> [0,127]
func Quantize(v float64) int8 {
	if v < 0 { return -128 }
	if v >= 1 { return 127 }
	return int8(v * 127)
}

func LoadBinary(path string) (*RefData, error) {
	f, err := os.Open(path)
	if err != nil { return nil, err }
	defer f.Close()
	
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return nil, err
	}
	if string(magic[:]) != "IVFR" {
		return nil, fmt.Errorf("bad magic")
	}

	var d uint8
	var count uint32
	var k uint16
	binary.Read(f, binary.LittleEndian, &d)
	binary.Read(f, binary.LittleEndian, &count)
	binary.Read(f, binary.LittleEndian, &k)
	if int(d) != DIM || int(k) != NUM_CLUSTERS {
		return nil, fmt.Errorf("file/binary mismatch")
	}

	rd := &RefData{
		centroids: 	make([]int8, int(k)*STRIDE),
		offsets:   	make([]uint32, int(k)+1),
		vectors:	make([]int8, int(count)*STRIDE),
		labels:		make([]uint8, count),
		count:		int(count),
	}

	cbuf := make([]byte, int(k)*DIM)
	io.ReadFull(f, cbuf)
	for c := 0; c < int(k); c++ {
		for j := 0; j < DIM; j++ {
			rd.centroids[c*STRIDE+j] = int8(cbuf[c*DIM+j])
		}
	} 

	binary.Read(f, binary.LittleEndian, rd.offsets)

	buf := make([]byte, DIM+1)
	for i := 0; i < int(count); i++ {
		if _, err := io.ReadFull(f, buf); err !=  nil {
			return nil, err
		}
		for j := 0; j < DIM; j++ {
			rd.vectors[i*STRIDE+j] = int8(buf[j])
		}
		rd.labels[i] = buf[DIM]
	}
	return rd, nil
}
