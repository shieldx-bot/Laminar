package cgvnode

import (
	"fmt"
	"hash/fnv"
	"sort"
)

type HashRing struct {
	ring     []uint32
	vnodeMap map[uint32]string
	vnodes   int
}

func hashKey(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

func NewHashRing(nodes []string, vnodes int) *HashRing {
	r := &HashRing{
		vnodeMap: make(map[uint32]string),
		vnodes:   vnodes,
	}

	for _, node := range nodes {
		for i := 0; i < vnodes; i++ {

			vnodeKey := fmt.Sprintf("%s#%d", node, i)
			h := hashKey(vnodeKey)
			r.ring = append(r.ring, h)
			r.vnodeMap[h] = node

		}
	}

	sort.Slice(r.ring, func(i, j int) bool {
		return r.ring[i] < r.ring[j]
	})

	return r
}

type ListNode struct {
	Key string
}

func (r *HashRing) GetNode(key string) []ListNode {
	if len(r.ring) == 0 {
		return nil
	}

	h := hashKey(key)

	idx := sort.Search(len(r.ring), func(i int) bool {
		return r.ring[i] >= h
	})

	if idx == len(r.ring) {
		idx = 0 // wrap around
	}
	List := []ListNode{
		{Key: r.vnodeMap[r.ring[idx]]},
		{Key: r.vnodeMap[r.ring[(idx+1)%len(r.ring)]]},
	}

	return List
}

// func main() {
// 	nodes := []string{
// 		"backend-A",
// 		"backend-B",
// 		"backend-C",
// 		"backend-D",
// 		"backend-E",
// 		"backend-F",
// 		"backend-G",
// 		"backend-H",
// 		"backend-I",
// 		"backend-J",
// 		"backend-K",
// 		"backend-L",
// 		"backend-M",
// 		"backend-N",
// 		"backend-O",
// 	}

// 	var listCong map[string]int
// 	listCong = map[string]int{
// 		"backend-A": 0,
// 		"backend-B": 0,
// 		"backend-C": 0,
// 		"backend-D": 0,
// 		"backend-E": 0,
// 		"backend-F": 0,
// 		"backend-G": 0,
// 		"backend-H": 0,
// 		"backend-I": 0,
// 		"backend-J": 0,
// 		"backend-K": 0,
// 		"backend-L": 0,
// 		"backend-M": 0,
// 		"backend-N": 0,
// 		"backend-O": 0,
// 	}

// 	ring := NewHashRing(nodes, 20)

// 	keys := []string{"user1", "user2", "user3", "user4", "user5", "user6", "user7", "user8", "user9", "user10", "user11", "user12", "user13", "user14", "user15", "user16", "user17", "user18", "user19", "user20", "user21", "user22", "user23", "user24", "user25", "user26", "user27", "user28", "user29", "user30", "user31", "user32", "user33", "user34", "user35", "user36", "user37", "user38", "user39", "user40", "user41", "user42", "user43", "user44", "user45", "user46", "user47", "user48", "user49", "user50", "user51", "user52", "user53", "user54", "user55", "user56", "user57", "user58", "user59", "user60", "user61", "user62", "user63", "user64", "user65", "user66", "user67", "user68", "user69", "user70", "user71", "user72", "user73", "user74", "user75", "user76", "user77", "user78", "user79", "user80", "user81", "user82", "user83", "user84", "user85", "user86", "user87", "user88", "user89", "user90", "user91", "user92", "user93", "user94", "user95", "user96", "user97", "user98", "user99", "user100"}

// 	for _, k := range keys {
// 		nodes := ring.GetNode(k)
// 		listCong[nodes[0].Key] += 1
// 		listCong[nodes[1].Key] += 1

// 	}
// 	// fmt.Printf("key=%s -> %s\n", k, ring.GetNode(k))
// 	fmt.Println("Load distribution across backend nodes:")
// 	for node, count := range listCong {
// 		fmt.Printf("%s: %d\n", node, count)
// 	}

// }
