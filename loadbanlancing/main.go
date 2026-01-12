package main

import (
	"bytes"
	"fmt"
	"github/shieldx-bot/loadbanlacing/agent"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type VPS struct {
	IP string
	S  float64 // score (defaults to 0)
	p  float64
}

var ListVPS = []VPS{}

func hasVPS(ip string) bool {
	for _, v := range ListVPS {
		if v.IP == ip {
			return true
		}
	}
	return false
}

var WeightMetrix = struct {
	a         float64
	b         float64
	c         float64
	p         float64
	k         float64
	Max_queue int
}{

	a:         0.2,
	b:         0.05,
	c:         0.05,
	k:         0.3,
	Max_queue: 1000,
}

type MetrixCalculateP struct {
	S     float64 `json:"s"`
	Gj    float64 `json:"gj"`
	Lvmj  float64 `json:"lvmj"`
	LB    int     `json:"lb"`
	Queue int     `json:"queue"`
}

func CalculateP(data MetrixCalculateP) float64 {
	var P float64
	var coalesce_penalty float64
	var coalesce_penalty_j float64
	coalesce_penalty = float64(data.Queue) / float64(WeightMetrix.Max_queue)
	coalesce_penalty_j = 1 - WeightMetrix.k*coalesce_penalty
	var learning_score_all float64
	for _, v := range ListVPS {
		learning_score_all += v.p
	}
	var learning_score float64
	learning_score = data.S * data.Gj * data.Lvmj * float64(data.LB) / learning_score_all
	P = learning_score * coalesce_penalty_j
	return P
}
func main() {
	router := gin.Default()
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong - load balancing",
		})
	})

	router.POST("/load-test-http3", func(c *gin.Context) {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(400, gin.H{"error": "cannot read body"})
			return
		}

		req, err := http.NewRequest(
			http.MethodPost,
			"http://23.124.22.44:8082/api/test-http3",
			bytes.NewReader(bodyBytes),
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "create request failed"})
			return
		}

		// copy headers (rất quan trọng)
		req.Header = c.Request.Header.Clone()
		req.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(502, gin.H{"error": "fetch failed"})
			return
		}
		defer resp.Body.Close()

		c.Status(resp.StatusCode)
		io.Copy(c.Writer, resp.Body)
	})

	router.POST("/receive-metrics", func(c *gin.Context) {
		type metricsIn struct {
			TimeDoneTask  int64   `json:"TimeDoneTask"`
			TimeStartSend int64   `json:"TimeStartSend"`
			Penumj        int64   `json:"Penumj"`
			Pemips        int64   `json:"Pemips"`
			NumberTask    int64   `json:"NumberTask"`
			TTj           float64 `json:"ttj"`
			TLi           int64   `json:"tli"`
			IPVM          string  `json:"ip_vm"`
			IFS           int     `json:"ifs"`
			VMbw          float64 `json:"vmbw"`
			TotalOnQueue  int64   `json:"total_on_queue"`
		}

		var in metricsIn
		timeStart := time.Now().UnixMilli()
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(400, gin.H{"error": "invalid json"})
			return
		}
		if in.TimeStartSend == 0 {
			c.JSON(400, gin.H{"error": "missing or invalid TimeStartSend"})
			return
		}

		Metrix := map[string]interface{}{
			"TimeDoneTask": in.TimeDoneTask,
			"Ttj":          timeStart - in.TimeStartSend,
			"Penumj":       in.Penumj,
			"Pemips":       in.Pemips,
			"NumberTask":   in.NumberTask,
			"TTj":          in.TTj,
			"TLi":          in.TLi,
			"IFS":          in.IFS,
			"VMbw":         in.VMbw,
			"IPVM":         in.IPVM,
			"TotalOnQueue": in.TotalOnQueue,
		}
		_, err := agent.AgentMain(Metrix)

		if err != nil {
			c.JSON(500, gin.H{"error": "agent processing failed"})
			return
		}
		if !hasVPS(in.IPVM) {
			ListVPS = append(ListVPS, VPS{IP: in.IPVM, S: 0, p: 0})
		} else {
			for i, v := range ListVPS {
				if v.IP == in.IPVM {
					if v.p == 0.0 {
						var S0 float64
						S0 = float64(Metrix["Penumj"].(int64))*float64(Metrix["Pemips"].(int64)) + Metrix["VMbw"].(float64)
						ListVPS[i].S = S0

						gj, _ := Metrix["Gj"].(float64)
						lvmj, _ := Metrix["Lvmj"].(float64)
						lb, _ := Metrix["LB"].(int)
						Queue, _ := Metrix["Queue"].(int)

						var p float64
						p = CalculateP(MetrixCalculateP{
							S:     S0,
							Gj:    gj,
							Lvmj:  lvmj,
							LB:    lb,
							Queue: Queue,
						})
						ListVPS[i].p = p

					} else {
						var Snew float64
						timeDoneTask := float64(in.TimeDoneTask)
						if timeDoneTask <= 0 {
							timeDoneTask = 1
						}
						gj, _ := Metrix["Gj"].(float64)
						lvmj, _ := Metrix["Lvmj"].(float64)
						lb, _ := Metrix["LB"].(int)
						Queue, _ := Metrix["Queue"].(int)
						Snew = (1-ListVPS[i].p)*ListVPS[i].S + (1.0/timeDoneTask)*1e-9
						ListVPS[i].S = Snew
						var p float64
						p = CalculateP(MetrixCalculateP{
							S:     Snew,
							Gj:    gj,
							Lvmj:  lvmj,
							LB:    lb,
							Queue: Queue,
						})
						ListVPS[i].p = p
					}
				}
			}
		}

		// Xử lý metrics ở đây (ví dụ: lưu vào cơ sở dữ liệu, in ra console, v.v.)
		// Hiện tại chỉ in ra console
		fmt.Println("Received metrics: \n ", Metrix)
		c.JSON(200, gin.H{"status": "metrics received"})
	})

	router.Run(":8083")

}
