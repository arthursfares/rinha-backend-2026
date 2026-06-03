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

//type Top3Finder func(query []int8, ref *RefData) [3]LabeledScore

// returns the 3 nearest reference vectors to query,
// sorted ascended by squared distance
func FindTop3(query []int8, ref *RefData) [3]LabeledScore {
	top := [3]LabeledScore{
		{Score: math.MaxInt32},
		{Score: math.MaxInt32},
		{Score: math.MaxInt32},
	}
	for i := 0; i < ref.count; i++ {
		distance := sqDistQuantized(query, i*STRIDE, ref.vectors)
		// skip if it can't beat the worst of the top 3
		if distance >= top[2].Score { continue }
		// insert at the worst slot, then bubble up to keep ascending order
		top[2] = LabeledScore{distance, ref.labels[i]}
        if top[2].Score < top[1].Score {
            top[1], top[2] = top[2], top[1]
        }
        if top[1].Score < top[0].Score {
            top[0], top[1] = top[1], top[0]
        }
	}
	return top
}


func FindTop3IVF(query []int8, ref *RefData) [3]LabeledScore {
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
	distancesMin3 := [3]LabeledScore{
		{Score: math.MaxInt32},
		{Score: math.MaxInt32},
		{Score: math.MaxInt32},
	}
	for _, p := range probes {
		start := int(ref.offsets[p.id])
		end := int(ref.offsets[p.id+1])
		for i := start; i < end; i++ {
			d := sqDistQuantized(query, i*STRIDE, ref.vectors)
			if d >= distancesMin3[2].Score { continue }
			distancesMin3[2] = LabeledScore{d, ref.labels[i]}
			if distancesMin3[2].Score < distancesMin3[1].Score {
				distancesMin3[1], distancesMin3[2] = distancesMin3[2], distancesMin3[1]
			}
			if distancesMin3[1].Score < distancesMin3[0].Score {
				distancesMin3[0], distancesMin3[1] = distancesMin3[1], distancesMin3[0]
			}
		}
	}
	return distancesMin3
}
