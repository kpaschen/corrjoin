package reporter

import (
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"log"
	"slices"
)

type CorrelatedSet struct {
	pairs   map[comparisons.RowPair]float64
	members []int // maintained in sort order
}

type Reporter struct {
	correlations []*CorrelatedSet
}

func (s *CorrelatedSet) contains(member int) bool {
	return slices.Contains(s.members, member)
}

func (s *CorrelatedSet) insert(member int) bool {
	if s.contains(member) {
		return false
	}
	i, _ := slices.BinarySearch(s.members, member)
	s.members = slices.Insert(s.members, i, member)
	return true
}

func NewReporter() *Reporter {
	return &Reporter{correlations: make([]*CorrelatedSet, 0, 10000)}
}

func (r *Reporter) PrintReport(tsids []string) {
	log.Printf("timeseries correlation report\n")
	for _, c := range r.correlations {
		log.Printf("correlated set with %d members\n", len(c.members))
		if len(c.members) < 100 {
			for i, m := range c.members {
				log.Printf("%d: %s\n", i, tsids[m])
			}
			for pair, score := range c.pairs {
				log.Printf("%+v: %f\n", pair, score)
			}
		}
	}
	r.correlations = make([]*CorrelatedSet, 0, 10000)
}

func (r *Reporter) AddCorrelatedPair(pair comparisons.RowPair, corr float64) error {
	ids := pair.RowIds()
	homeForT1 := -1
	homeForT2 := -1
	// Cases:
	// 1. neither of them is in a set yet --> create one for them
	// 2. t1 and t2 are already in the same set --> add their pair to that set
	// 3. t1 is in a set, t2 isn't or vice versa --> add their pair and the new member to the set
	// 4. they are in different sets --> merge those two sets
	for idx, set := range r.correlations {
		if homeForT1 < 0 && set.contains(ids[0]) {
			homeForT1 = idx
		}
		if homeForT2 < 0 && set.contains(ids[1]) {
			homeForT2 = idx
		}
		if homeForT1 >= 0 && homeForT2 >= 0 {
			break
		}
	}
	// Case 1: new set needs to be created
	if homeForT1 < 0 && homeForT2 < 0 {
		newset := &CorrelatedSet{
			pairs:   map[comparisons.RowPair]float64{pair: corr},
			members: make([]int, 0, 100),
		}
		newset.insert(ids[0])
		newset.insert(ids[1])
		r.correlations = append(r.correlations, newset)
		return nil
	}
	// Case 2: already in same set
	if homeForT1 == homeForT2 {
		r.correlations[homeForT1].pairs[pair] = corr
		return nil
	}
	// Case 3: one of them is in a set
	if homeForT1 < 0 && homeForT2 >= 0 {
		r.correlations[homeForT2].pairs[pair] = corr
		r.correlations[homeForT2].insert(ids[0])
		return nil
	}
	if homeForT2 < 0 && homeForT1 >= 0 {
		r.correlations[homeForT1].pairs[pair] = corr
		r.correlations[homeForT1].insert(ids[1])
		return nil
	}
	// Case 4: They are in different sets
	if homeForT1 >= 0 && homeForT2 >= 0 && homeForT1 != homeForT2 {
		for p, c := range r.correlations[homeForT2].pairs {
			r.correlations[homeForT1].pairs[p] = c
		}
		r.correlations[homeForT1].pairs[pair] = corr
		for _, m := range r.correlations[homeForT2].members {
			r.correlations[homeForT1].insert(m)
		}
		r.correlations[homeForT2].pairs = make(map[comparisons.RowPair]float64)
		r.correlations[homeForT2].members = make([]int, 0, 0)
		return nil
	}

	return nil
}
