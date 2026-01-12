package metrix

import (
	"bytes"
	"net/http"
	"time"
)

type MetrixFirst struct {
	TimeStart int64 `json:"time_start"`
}

type MetrixEnd struct {
	TimeEnd            int64   `json:"time_end"`
	TimeTransportStart int64   `json:"time_transport_start"`
	Pemips             int64   `json:"pemips"`
	IFS                int     `json:"ifs"`
	VMbw               float64 `json:"vmbw"`
	TTj                float64 `json:"ttj"`
}

func MetrixFirstFunction() MetrixFirst {
	TimeStart := time.Now().UnixNano() / int64(time.Millisecond)
	return MetrixFirst{
		TimeStart: TimeStart,
	}
}

type CalculateTTjResult struct {
	IFS  int     // bytes
	VMbw float64 // bytes/sec
}

func CalculateTTj() CalculateTTjResult {
	payload := make([]byte, 1024*50) // 50KB

	start := time.Now()
	resp, _ := http.Post(
		"http://127.0.0.1:8080/echo",
		"application/json",
		bytes.NewReader(payload),
	)
	_ = resp
	elapsed := time.Since(start).Seconds()

	IFS := len(payload)            // bytes
	VMbw := float64(IFS) / elapsed // bytes/sec

	return CalculateTTjResult{
		IFS:  IFS,
		VMbw: VMbw,
	}

}

func MetrixEndFunction() MetrixEnd {

	TimeEnd := time.Now().UnixNano() / int64(time.Millisecond)

	Pemips := CalculatePemips()
	TTj := CalculateTTj()

	return MetrixEnd{
		TimeEnd: TimeEnd,
		Pemips:  Pemips,
		IFS:     TTj.IFS,
		VMbw:    TTj.VMbw,
		TTj:     float64(TTj.IFS) / TTj.VMbw,
	}
}

func CalculatePemips() int64 {
	start := time.Now()
	CpuTask()
	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		elapsed = 1e-9 // tránh chia cho 0
	}
	Pemips := int64(1 / elapsed)
	return Pemips
}

func CpuTask() {
	x := 0
	for i := 0; i < 100000000; i++ {
		x += i
	}
}
