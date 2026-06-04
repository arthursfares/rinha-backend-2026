package main

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"sync"
)

type Config struct {
	Vector	[]float64 	`json:"vector"`
	Label 	string		`json:"label"`
}

const (
	DIM				= 14
	NUM_CLUSTERS	= 1024
	SAMPLE_SIZE		= 50000
	MAX_ITER		= 15
)

// func quantize(v float64) int8 {
// 	if v < 0 { return -128 }
// 	if v >= 1 { return 127 }
// 	return int8(v * 127)
// }

func sqDistF(a, b []float32) float32 {
	var s float32
	for i:= 0; i < DIM; i++ {
		d := a[i] - b[i]
		s += d * d
	}
	return s
}

func nearest(v, centroids []float32, k int) int {
	bestVector, bestDistance := 0, float32(1e30)
	for c := 0; c < k; c++ {
		distance := sqDistF(v, centroids[c*DIM:(c+1)*DIM])
		if distance < bestDistance {
			bestDistance, bestVector = distance, c
		}
	}
	return bestVector
}


func kmeans(vecs []float32, numVectors, numClusters int) []float32 {
	randNumGenerator := rand.New(rand.NewSource(666))
	shuffledIndices := randNumGenerator.Perm(numVectors)

	centroids := make([]float32, numClusters*DIM)
	for clusterIdx := 0; clusterIdx < numClusters; clusterIdx++ {
		srcStart := shuffledIndices[clusterIdx] * DIM
		copy(centroids[clusterIdx*DIM:(clusterIdx+1)*DIM], vecs[srcStart:srcStart+DIM])
	}

	sampleIndices := shuffledIndices[:SAMPLE_SIZE]
	for iter := 0; iter < MAX_ITER; iter++ {
		clusterSums := make([]float32, numClusters*DIM)
		clusterCounts := make([]int, numClusters)
		
		for _, vecIdx := range sampleIndices {
			vec := vecs[vecIdx*DIM : (vecIdx+1)*DIM]
			nearestCluster := nearest(vec, centroids, numClusters)
			clusterCounts[nearestCluster]++
			for d := 0; d < DIM; d++ {
				clusterSums[nearestCluster*DIM+d] += vec[d]	
			}
		}

		for clusterIdx := 0; clusterIdx < numClusters; clusterIdx++ {
			if clusterCounts[clusterIdx] > 0 {
				for d := 0; d < DIM; d++ {
					centroids[clusterIdx*DIM+d] = clusterSums[clusterIdx*DIM+d] / float32(clusterCounts[clusterIdx])
				}
			}
		}
		
		log.Printf(" iter %d", iter+1)
	}

	return centroids
}

func assignAll(vecs []float32, n int, centroids []float32) []uint16 {
	k := len(centroids) / DIM
	assignments := make([]uint16, n)
	workers := 8
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start, end := worker*chunk, (worker+1)*chunk
		if end < n { end = n }
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				assignments[i] = uint16(nearest(vecs[i*DIM:(i+1)*DIM], centroids, k))
			}
		}(start, end)
	}
	wg.Wait()
	return assignments
}


// - centroids as float32 (fast SIMD cluster selection at query time)
// - vectors as int16 scaled by 32767 (precision ~0.00003, half the bytes
// of float32 → ~87 MB on disk, ~96 MB in RAM vs 192 MB for float32)
func writeBinary(path string, data []Config, vecs []float32, order []int,
	centroids []float32, offsets []uint32, n int) {
	f, _ := os.Create(path)
	defer f.Close()
	w := bufio.NewWriterSize(f, 4<<20)
	defer w.Flush()

	w.Write([]byte("IVFS"))
	binary.Write(w, binary.LittleEndian, uint8(DIM))
	binary.Write(w, binary.LittleEndian, uint32(n))
	binary.Write(w, binary.LittleEndian, uint16(NUM_CLUSTERS))

	// centroids as float32 (only 14 KB, worth the precision for cluster search)
	binary.Write(w, binary.LittleEndian, centroids[:NUM_CLUSTERS*DIM])

	// offsets
	binary.Write(w, binary.LittleEndian, offsets)

	// vectors as int16 + 1 byte label; DIM*2+1 = 29 bytes per entry
	buf := make([]byte, DIM*2+1)
	for _, idx := range order {
		for j := 0; j < DIM; j++ {
			v := vecs[idx*DIM+j] * 32767.0
			if v > 32767 {
				v = 32767
			} else if v < -32767 {
				v = -32767
			}
			binary.LittleEndian.PutUint16(buf[j*2:], uint16(int16(math.Round(float64(v)))))
		}
		if data[idx].Label == "fraud" {
			buf[DIM*2] = 1
		} else {
			buf[DIM*2] = 0
		}
		w.Write(buf)
	}
}

func loadJSON(path string) []Config {
	f, _ := os.Open(path)
	defer f.Close()
	var r io.Reader = f
	if gz, err := gzip.NewReader(f); err == nil {
		defer gz.Close()
		r = gz
	}
	var data []Config
	json.NewDecoder(r).Decode(&data)
	return data
}


func main() {
	log.Println("loading json.gz")
	data := loadJSON("references.json.gz")
	n := len(data)
	vecs := make([]float32, n*DIM)
	for i, c:= range data {
		for j := 0; j < DIM; j++ {
			vecs[i*DIM+j] = float32(c.Vector[j])
		}
	}
	log.Printf("running k-means on %d sample of %d vectors", SAMPLE_SIZE, n)
	centroids := kmeans(vecs, n, NUM_CLUSTERS)
	log.Println("assigning all vectors to clusters")
	assignments := assignAll(vecs, n, centroids)
	log.Println("computing offsets and reordering")
	counts := make([]uint32, NUM_CLUSTERS)
	for _, c := range assignments {
		counts[c]++
	}
	offsets := make([]uint32, NUM_CLUSTERS+1)
	for i := 0; i < NUM_CLUSTERS; i++ {
		offsets[i+1] = offsets[i] + counts[i]
	}
	order := make([]int, n)
	cursors := make([]uint32, NUM_CLUSTERS)
	copy(cursors, offsets[:NUM_CLUSTERS])
	for i := 0; i < n; i++ {
		c := assignments[i]
		order[cursors[c]] = i
		cursors[c]++
	}
	log.Println("writing references.bin")
	writeBinary("references.bin", data, vecs, order, centroids, offsets, n)
	log.Println("done")
}
