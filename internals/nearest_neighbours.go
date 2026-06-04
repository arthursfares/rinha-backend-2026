package internals

import "math"

const (
	NPROBE = 8  // how many clusters to scan per query
)

type LabeledScore struct {
	Score	int32
	Label	uint8
}

//func sqDist16(q, r *[STRIDE]int8) int32 {
//	var sum int32
//	for i:= 0; i < DIM; i++ {
//		d := int32(q[i]) - int32(r[i])
//		sum += d * d
//	}
//	return sum
//}

func sqDistQuantized(query []int8, refOffset int, ref []int8) int32 {
	//q := (*[STRIDE]int8)(query[:STRIDE])
	//r := (*[STRIDE]int8)(ref[refOffset : refOffset+STRIDE])
	//return sqDist16(q, r)
	return sqDistSIMD(&query[0], &ref[refOffset])
}

//func sqDistQuantized(query []int8, refOffset int, ref []int8) int32 {
//	q := query[:DIM]
//	r := ref[refOffset : refOffset+DIM]
//	var sum int32
//	for i := 0; i < DIM; i++ {
//		d := int32(q[i]) - int32(r[i])
//		sum += d * d
//	}
//	return sum
//}

func FindTop5(query []int8, ref *RefData) [5]LabeledScore {
	top := [5]LabeledScore{
		{Score: math.MaxInt32}, {Score: math.MaxInt32}, {Score: math.MaxInt32},
		{Score: math.MaxInt32}, {Score: math.MaxInt32},
	}
	for i := 0; i < ref.count; i++ {
		distance := sqDistQuantized(query, i*STRIDE, ref.vectors)
		if distance >= top[4].Score {
			continue
		}
		j := 4
		for j > 0 && distance < top[j-1].Score {
			top[j] = top[j-1]
			j--
		}
		top[j] = LabeledScore{distance, ref.labels[i]}
	}
	return top
}

func FindTop5IVF(query []int8, ref *RefData) [5]LabeledScore {
	// find NPROBE closest centroids
	type probe struct {
		dist 	int32
		id 		int
	}
	probes := [NPROBE]probe{}
	for i := range probes {
		probes[i].dist = math.MaxInt32
	}
	for c := 0; c < NUM_CLUSTERS; c++ {
		d := sqDistQuantized(query, c*STRIDE, ref.centroids)
		if d >= probes[NPROBE-1].dist { continue }
		probes[NPROBE-1] = probe{d, c}
		// bubble up
		for k := NPROBE - 1; k > 0 && probes[k].dist < probes[k-1].dist; k-- {
			probes[k], probes[k-1] = probes[k-1], probes[k]
		}
	}
	// scan only those clusters
	top := [5]LabeledScore{
		{Score: math.MaxInt32}, {Score: math.MaxInt32}, {Score: math.MaxInt32},
		{Score: math.MaxInt32}, {Score: math.MaxInt32},
	}
	for _, p := range probes {
		start := int(ref.offsets[p.id])
		end := int(ref.offsets[p.id+1])
		for i := start; i < end; i++ {
			d := sqDistQuantized(query, i*STRIDE, ref.vectors)
			if d >= top[4].Score { continue }
			j := 4
			for j > 0 && d < top[j-1].Score {
				top[j] = top[j-1]
				j--
			}
			top[j] = LabeledScore{d, ref.labels[i]}
		}
	}
	return top
}
