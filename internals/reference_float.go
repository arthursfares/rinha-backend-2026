package internals

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// RefDataFloat stores centroids as float32 (for fast SIMD cluster selection)
// and vectors as int16 scaled by 32767 — precision ~0.00003, half the bytes
// of float32.  3M vectors × STRIDE × 2 bytes ≈ 96 MB
type RefDataFloat struct {
	centroids []float32 // NUM_CLUSTERS * STRIDE, zero-padded to lane 16
	offsets   []uint32  // NUM_CLUSTERS + 1
	vectors   []int16   // count * STRIDE, scaled by 32767; lanes 14,15 = 0
	labels    []uint8
	count     int
}

func dequantize(v int8) float32 {
	if v == -128 {
		return -1.0
	}
	return float32(v) / 127.0
}

// quantizeI16 maps [-1,1] → [-32767,32767] with rounding.
func quantizeI16(v float32) int16 {
	s := v * 32767.0
	if s >= 32767.0 {
		return 32767
	}
	if s <= -32767.0 {
		return -32767
	}
	return int16(math.Round(float64(s)))
}

// LoadBinaryFloat supports two on-disk formats:
//   - "IVFR"  legacy int8-quantized (converted to int16 on load)
//   - "IVFS"  int16-quantized  (read directly, ~87 MB on disk)
func LoadBinaryFloat(path string) (*RefDataFloat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return nil, err
	}
	switch string(magic[:]) {
	case "IVFR", "IVFS":
	default:
		return nil, fmt.Errorf("unsupported format: %q (expected IVFR or IVFS)", string(magic[:]))
	}
	int16Format := string(magic[:]) == "IVFS"

	var d uint8
	var count uint32
	var k uint16
	binary.Read(f, binary.LittleEndian, &d)
	binary.Read(f, binary.LittleEndian, &count)
	binary.Read(f, binary.LittleEndian, &k)
	if int(d) != DIM || int(k) != NUM_CLUSTERS {
		return nil, fmt.Errorf("file/binary mismatch: DIM=%d k=%d", d, k)
	}

	// make() zero-initialises: lanes 14 and 15 of every vector stay 0.
	rd := &RefDataFloat{
		centroids: make([]float32, int(k)*STRIDE),
		offsets:   make([]uint32, int(k)+1),
		vectors:   make([]int16, int(count)*STRIDE),
		labels:    make([]uint8, count),
		count:     int(count),
	}

	if int16Format {
		// IVFS: centroids are float32, vectors are int16
		cbuf := make([]byte, int(k)*DIM*4)
		io.ReadFull(f, cbuf)
		for c := range int(k) {
			for j := range DIM {
				rd.centroids[c*STRIDE+j] = math.Float32frombits(
					binary.LittleEndian.Uint32(cbuf[(c*DIM+j)*4:]))
			}
		}

		binary.Read(f, binary.LittleEndian, rd.offsets)

		// Buffered streaming: 1 MB window avoids a ~87 MB temp allocation.
		br := bufio.NewReaderSize(f, 1<<20)
		entryBuf := make([]byte, DIM*2+1)
		for i := range int(count) {
			if _, err := io.ReadFull(br, entryBuf); err != nil {
				return nil, err
			}
			for j := range DIM {
				rd.vectors[i*STRIDE+j] = int16(binary.LittleEndian.Uint16(entryBuf[j*2:]))
			}
			rd.labels[i] = entryBuf[DIM*2]
		}
	} else {
		// IVFR: legacy int8 centroids and vectors
		cbuf := make([]byte, int(k)*DIM)
		io.ReadFull(f, cbuf)
		for c := range int(k) {
			for j := range DIM {
				rd.centroids[c*STRIDE+j] = dequantize(int8(cbuf[c*DIM+j]))
			}
		}

		binary.Read(f, binary.LittleEndian, rd.offsets)

		buf := make([]byte, DIM+1)
		for i := range int(count) {
			if _, err := io.ReadFull(f, buf); err != nil {
				return nil, err
			}
			for j := range DIM {
				rd.vectors[i*STRIDE+j] = quantizeI16(dequantize(int8(buf[j])))
			}
			rd.labels[i] = buf[DIM]
		}
	}
	return rd, nil
}

type LabeledScoreFloat struct {
	Score float32
	Label uint8
}

const invScaleI16 float32 = 1.0 / 32767.0

// sqDistI16 computes squared Euclidean distance between a float32 query and
// an int16-scaled reference vector.  Centroids use sqDistSIMDFloat instead.
func sqDistI16(query *[STRIDE]float32, ref []int16, offset int) float32 {
	var sum float32
	for j := range DIM {
		d := query[j] - float32(ref[offset+j])*invScaleI16
		sum += d * d
	}
	return sum
}

// FindTop5Float is the brute-force version. query is the raw output of
// NormalizeValues (no quantization applied).
func FindTop5Float(query []float64, ref *RefDataFloat) [5]LabeledScoreFloat {
	var qf [STRIDE]float32
	for i, v := range query[:DIM] {
		qf[i] = float32(v)
	}
	top := [5]LabeledScoreFloat{
		{Score: math.MaxFloat32}, {Score: math.MaxFloat32}, {Score: math.MaxFloat32},
		{Score: math.MaxFloat32}, {Score: math.MaxFloat32},
	}
	for i := range ref.count {
		d := sqDistI16(&qf, ref.vectors, i*STRIDE)
		if d >= top[4].Score {
			continue
		}
		j := 4
		for j > 0 && d < top[j-1].Score {
			top[j] = top[j-1]
			j--
		}
		top[j] = LabeledScoreFloat{d, ref.labels[i]}
	}
	return top
}

// FindTop5IVFFloat is the IVF version. Centroid selection uses the float32
// SIMD path; per-vector distances use sqDistI16 over the int16 vectors.
func FindTop5IVFFloat(query []float64, ref *RefDataFloat) [5]LabeledScoreFloat {
	var qf [STRIDE]float32
	for i, v := range query[:DIM] {
		qf[i] = float32(v)
	}

	type probe struct {
		dist float32
		id   int
	}
	probes := [NPROBE]probe{}
	for i := range probes {
		probes[i].dist = math.MaxFloat32
	}
	for c := range NUM_CLUSTERS {
		d := sqDistSIMDFloat(&qf[0], &ref.centroids[c*STRIDE])
		if d >= probes[NPROBE-1].dist {
			continue
		}
		probes[NPROBE-1] = probe{d, c}
		for kk := NPROBE - 1; kk > 0 && probes[kk].dist < probes[kk-1].dist; kk-- {
			probes[kk], probes[kk-1] = probes[kk-1], probes[kk]
		}
	}

	top := [5]LabeledScoreFloat{
		{Score: math.MaxFloat32}, {Score: math.MaxFloat32}, {Score: math.MaxFloat32},
		{Score: math.MaxFloat32}, {Score: math.MaxFloat32},
	}
	for _, p := range probes {
		start := int(ref.offsets[p.id])
		end := int(ref.offsets[p.id+1])
		for i := start; i < end; i++ {
			d := sqDistI16(&qf, ref.vectors, i*STRIDE)
			if d >= top[4].Score {
				continue
			}
			j := 4
			for j > 0 && d < top[j-1].Score {
				top[j] = top[j-1]
				j--
			}
			top[j] = LabeledScoreFloat{d, ref.labels[i]}
		}
	}
	return top
}
