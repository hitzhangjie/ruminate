package wiki

import (
	"math"
)

// mmrDiversify selects k diverse results from candidates using Maximal Marginal
// Relevance (MMR). It balances relevance to the query against similarity to
// already-selected documents, preventing a single semantic cluster from
// dominating the results.
//
// Parameters:
//   - queryVec: the query embedding for relevance computation
//   - candidates: pool of results with their embedding vectors
//   - lambda: relevance vs diversity tradeoff (0-1). Higher values favor
//     relevance; lower values favor diversity. 0.7 is a reasonable default.
//   - k: number of results to select
//
// Returns up to k diverse results. If candidates has fewer than k items,
// all candidates are returned in their original order.
func mmrDiversify(queryVec []float32, candidates []scoredResult, lambda float64, k int) []SearchResult {
	if len(candidates) == 0 || k <= 0 {
		return nil
	}

	// MMR: argmax [ λ·sim(d, query) - (1-λ)·max sim(d, already_selected) ]

	// Pre-compute cosine similarity to query for each candidate.
	// Both relevance and diversity terms are cosine similarities in [0,1],
	// so lambda directly controls the tradeoff.
	type mmrCandidate struct {
		scoredResult
		querySim float64 // cosine similarity to query
	}

	pool := make([]mmrCandidate, len(candidates))
	for i, c := range candidates {
		pool[i] = mmrCandidate{
			scoredResult: c,
			querySim:     float64(cosineSimilarity(queryVec, c.vector)),
		}
	}

	// First selection: pick the candidate with highest query similarity.
	bestIdx := 0
	bestSim := pool[0].querySim
	for i := 1; i < len(pool); i++ {
		if pool[i].querySim > bestSim {
			bestSim = pool[i].querySim
			bestIdx = i
		}
	}
	selected := []mmrCandidate{pool[bestIdx]}
	// Remove selected from pool.
	pool = append(pool[:bestIdx], pool[bestIdx+1:]...)

	// Track max similarity to selected set for each pool candidate.
	maxSimToSelected := make([]float64, len(pool))
	for i, c := range pool {
		maxSimToSelected[i] = float64(cosineSimilarity(c.vector, selected[0].vector))
	}

	for len(selected) < k && len(pool) > 0 {
		bestIdx := 0
		bestMMR := -math.MaxFloat64

		for i, c := range pool {
			mmrScore := lambda*c.querySim - (1.0-lambda)*maxSimToSelected[i]
			if mmrScore > bestMMR {
				bestMMR = mmrScore
				bestIdx = i
			}
		}

		// Move best candidate from pool to selected.
		chosen := pool[bestIdx]
		selected = append(selected, chosen)

		// Remove from pool.
		pool = append(pool[:bestIdx], pool[bestIdx+1:]...)
		maxSimToSelected = append(maxSimToSelected[:bestIdx], maxSimToSelected[bestIdx+1:]...)

		// Update max similarity for remaining candidates against the newly selected doc.
		for i, c := range pool {
			sim := float64(cosineSimilarity(c.vector, chosen.vector))
			if sim > maxSimToSelected[i] {
				maxSimToSelected[i] = sim
			}
		}
	}

	// Convert to SearchResult (drop vector and score metadata).
	out := make([]SearchResult, len(selected))
	for i, s := range selected {
		out[i] = s.SearchResult
	}
	return out
}
