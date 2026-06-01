package main

import (
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"os"
	"math/rand"
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

func quantize(v float64) int8 {
	if v < 0 { return -128 }
	if v >= 1 { return 127 }
	return int8(v * 127)
}

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

func writeBinary(path string, data []Config, vecs []float32, order []int,
	centroids []float32, offsets []uint32, n int) {
	f, _ := os.Create(path)
	defer f.Close()
	f.Write([]byte("IVFR"))
	binary.Write(f, binary.LittleEndian, uint8(DIM))
	binary.Write(f, binary.LittleEndian, uint32(n))
	binary.Write(f, binary.LittleEndian, uint16(NUM_CLUSTERS))
	// quantized centroids
	cbuf := make([]byte, NUM_CLUSTERS*DIM)
	for i := 0; i < NUM_CLUSTERS*DIM; i++ {
		cbuf[i] = byte(quantize(float64(centroids[i])))
	}
	f.Write(cbuf)
	// offsets
	binary.Write(f, binary.LittleEndian, offsets)
	// vectors + labels in cluster order
	buf := make([]byte, DIM+1)
	for _, idx := range order {
		for j := 0; j < DIM; j++ {
			buf[j] = byte(quantize(float64(vecs[idx*DIM+j])))
		}
		if data[idx].Label == "fraud" {
			buf[DIM] = 1
		} else {
			buf[DIM] = 0
		}
		f.Write(buf)
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
