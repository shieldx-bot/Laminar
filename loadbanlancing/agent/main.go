package agent

import "fmt"

var (
	SLA float64 = 1.5 // 1.5 seconds to accommodate simulated tasks
)

type Agent struct {
	Gj   float64
	Lvmj float64
	LB   int
}

func AgentMain(metrix map[string]interface{}) (Agent, error) {
	Gj := CalculateGj(metrix)
	_ = Gj
	Lvmj := CalculateLvmj(metrix)
	_ = Lvmj
	LB := CalculateLB(metrix)
	_ = LB

	return Agent{
		Gj:   Gj,
		Lvmj: Lvmj,
		LB:   LB,
	}, nil
}

func CalculateETj(metrix map[string]interface{}) float64 {
	var ETj float64
	var TLi float64
	TLi = float64(metrix["TLi"].(int64))
	ETj = (TLi) / (float64(metrix["Penumj"].(int64)) * float64(metrix["Pemips"].(int64)))
	return ETj
}

func CalculateGj(metrix map[string]interface{}) float64 {
	var Gj float64
	Gj = CalculateETj(metrix) + metrix["TTj"].(float64)

	return Gj
}

func CalculateLvmj(metrix map[string]interface{}) float64 {
	var Lvmj float64
	Lvmj = (float64(metrix["TLi"].(int64)) / float64(metrix["Pemips"].(int64)))
	return Lvmj

}

func CalculateLB(metrix map[string]interface{}) int {
	var LB int
	var TLD float64
	var MaxCapity float64
	MaxCapity = (SLA * 1000) * float64(metrix["Pemips"].(int64))
	TLD = float64(CalculateTLD(metrix)) // milliseconds
	fmt.Printf("TLD : %f\n", TLD)
	fmt.Printf("MaxCapity : %f\n", MaxCapity)
	if TLD < MaxCapity {
		LB = 1
	} else {
		LB = 0
	}
	return LB
}

func CalculateTLD(metrix map[string]interface{}) float64 {
	var TLD float64
	TLD = float64(metrix["TimeDoneTask"].(int64)) + float64(metrix["TLi"].(int64))
	return TLD
}
