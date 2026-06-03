package main

import (
	"io"
	//"encoding/json"
	"log"
	"net/http"
	//"net/http/pprof"
	"reflect"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/arthursfares/rinha-backend-2026/internals"
)

var sonicAPI = sonic.ConfigFastest

type scoreResponse struct {
	Approved   bool  `json:"approved"`
	FraudScore float64 `json:"fraud_score"`
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

func main() {
	referenceDataset, err := internals.LoadBinary("references.bin")
	if err != nil {
		log.Fatalf("failed to load reference dataset: %v", err)
	}
	
	// compile sonic's encode/decode codecs for the schemas
	if err := sonic.Pretouch(reflect.TypeOf(internals.Event{})); err != nil {
		log.Fatalf("pretouch Event: %v", err)
	}
	if err := sonic.Pretouch(reflect.TypeOf(scoreResponse{})); err != nil {
		log.Fatalf("pretouch scoreResponse: %v", err)
	}

	// ----
//	go func() {
//		pp := http.NewServeMux()
//		pp.HandleFunc("/debug/pprof/", pprof.Index)
//		pp.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
//		pp.HandleFunc("/debug/pprof/profile", pprof.Profile)
//		pp.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
//		pp.HandleFunc("/debug/pprof/trace", pprof.Trace)
//		log.Println("pprof on :6060")
//		log.Println(http.ListenAndServe(":6060", pp))
//	}()
	// ----

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", getReady)
	mux.HandleFunc("POST /fraud-score", postFraudScore(referenceDataset))

	server := &http.Server{
		Addr:    ":9999",
		Handler: mux,
	}
	log.Fatal(server.ListenAndServe())
}

func getReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ready"}`))
}

func postFraudScore(referenceDataset *internals.RefData) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bp := bufPool.Get().(*[]byte)
		buf := (*bp)[:0]
		body, err := readAll(r.Body, buf)
		if err != nil {
			*bp = body
			bufPool.Put(bp)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var testBlock internals.Event
		if err := sonicAPI.Unmarshal(body, &testBlock); err != nil {
			*bp = body
			bufPool.Put(bp)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		*bp = body
		bufPool.Put(bp)

		normBlock := internals.NormalizeValues(testBlock)
		var query [internals.STRIDE]int8	// 16-wide (14 and 15 stay zero)
		for i, v := range normBlock {
			query[i] = internals.Quantize(v)
		}
		distancesMin3 := internals.FindTop3(query[:], referenceDataset)
		fraudsCount := 0
		for _, res := range distancesMin3 {
			if res.Label == 1 {
				fraudsCount++
			}
		}
		approved := true
		fraudScore := float64(fraudsCount) / 3
		if fraudsCount >= 2 {
			approved = false
		}

		out, err := sonicAPI.Marshal(scoreResponse{
			Approved:	approved,
			FraudScore:	fraudScore,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}
}

// reads r fuly into dst (reusing its capacity), growing as needed
// equivalent to io.ReadAll, but allows recycling the backing array
func readAll(r io.Reader, dst []byte) ([]byte, error) {
	for {
		if len(dst) == cap(dst) {
			dst = append(dst, 0)[:len(dst)]
		}
		n, err := r.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return dst, err
		}
	}
}
