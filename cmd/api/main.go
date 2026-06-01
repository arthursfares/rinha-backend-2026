package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/arthursfares/rinha-backend-2026/internals"
)

type scoreResponse struct {
	Approved   bool  `json:"approved"`
	FraudScore float64 `json:"fraud_score"`
}

func main() {
	referenceDataset, err := internals.LoadBinary("references.bin")
	if err != nil {
		log.Fatalf("failed to load reference dataset: %v", err)
	}
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
		var testBlock internals.Event
		if err := json.NewDecoder(r.Body).Decode(&testBlock); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		normBlock := internals.NormalizeValues(testBlock)
		var query [internals.DIM]int8
		for i, v := range normBlock {
			query[i] = internals.Quantize(v)
		}
		distancesMin3 := internals.FindTop3IVF(query[:], referenceDataset)
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scoreResponse{
			Approved:   approved,
			FraudScore: fraudScore,
		})
	}
}



