package main

import (
	"bytes"
	"fmt"
	"github/shieldx-bot/loadbanlacing/agent"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type VPS struct {
	IP string
	S  float64 // score (defaults to 0)
	p  float64
}

var ListVPS = []VPS{
	{IP: "34.87.59.164", S: 0, p: 0},
	{IP: "34.158.51.160", S: 0, p: 0},
}
var RamdomVPS bool = true

var listVPSMu sync.RWMutex

func hasVPS(ip string) bool {
	listVPSMu.RLock()
	defer listVPSMu.RUnlock()
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
	S                float64 `json:"s"`
	Gj               float64 `json:"gj"`
	Lvmj             float64 `json:"lvmj"`
	LB               int     `json:"lb"`
	Queue            int     `json:"queue"`
	LearningScoreAll float64 `json:"learning_score_all"`
}

func CalculateP(data MetrixCalculateP) float64 {
	var P float64
	var coalesce_penalty float64
	var coalesce_penalty_j float64
	coalesce_penalty = float64(data.Queue) / float64(WeightMetrix.Max_queue)
	coalesce_penalty_j = 1 - WeightMetrix.k*coalesce_penalty
	learning_score_all := data.LearningScoreAll
	if learning_score_all <= 0 {
		learning_score_all = 1
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

		if !RamdomVPS {
			c.JSON(503, gin.H{"error": "load balancer disabled"})
			return
		}

		// 1️⃣ đọc snapshot VPS (RLock)
		listVPSMu.RLock()
		if len(ListVPS) == 0 {
			listVPSMu.RUnlock()
			c.JSON(503, gin.H{"error": "no vps available"})
			return
		}

		var selected *VPS

		// ưu tiên VPS chưa có score
		for i := range ListVPS {
			if ListVPS[i].p == 0.0 || ListVPS[i].S == 0.0 {
				selected = &ListVPS[i]
				break
			}
		}

		// nếu không có → chọn p lớn nhất
		if selected == nil {
			selected = &ListVPS[0]
			for i := 1; i < len(ListVPS); i++ {
				if ListVPS[i].p > selected.p {
					selected = &ListVPS[i]
				}
			}
		}

		ip := selected.IP
		listVPSMu.RUnlock()

		// 2️⃣ gửi request (KHÔNG LOCK)
		req, err := http.NewRequest(
			http.MethodPost,
			"http://"+ip+":8081/http3-proxy",
			bytes.NewReader(bodyBytes),
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "create request failed"})
			return
		}

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
			"Queue":        int(in.TotalOnQueue),
		}
		ag, err := agent.AgentMain(Metrix)

		if err != nil {
			c.JSON(500, gin.H{"error": "agent processing failed"})
			return
		}
		Metrix["Gj"] = ag.Gj
		Metrix["Lvmj"] = ag.Lvmj
		Metrix["LB"] = ag.LB

		listVPSMu.Lock()
		idx := -1
		for i := range ListVPS {
			if ListVPS[i].IP == in.IPVM {
				idx = i
				break
			}
		}
		if idx == -1 {
			ListVPS = append(ListVPS, VPS{IP: in.IPVM, S: 0, p: 0})
			idx = len(ListVPS) - 1
		}

		learningScoreAll := 0.0
		for _, v := range ListVPS {
			learningScoreAll += v.p
		}

		gj := ag.Gj
		lvmj := ag.Lvmj
		lb := ag.LB
		Queue := int(in.TotalOnQueue)

		if ListVPS[idx].p == 0.0 {
			S0 := float64(in.Penumj)*float64(in.Pemips) + float64(in.VMbw)
			ListVPS[idx].S = S0

			p := CalculateP(MetrixCalculateP{
				S:                S0,
				Gj:               gj,
				Lvmj:             lvmj,
				LB:               lb,
				Queue:            Queue,
				LearningScoreAll: learningScoreAll,
			})
			ListVPS[idx].p = p
			fmt.Println("Received p: \n ", p)
		} else {
			timeDoneTask := float64(in.TimeDoneTask)
			if timeDoneTask <= 0 {
				timeDoneTask = 1
			}
			eps := 1e-9
			reward := 1.0 / (timeDoneTask + eps)
			Snew := (1-ListVPS[idx].p)*ListVPS[idx].S + ListVPS[idx].p*reward
			ListVPS[idx].S = Snew

			p := CalculateP(MetrixCalculateP{
				S:                Snew,
				Gj:               gj,
				Lvmj:             lvmj,
				LB:               lb,
				Queue:            Queue,
				LearningScoreAll: learningScoreAll,
			})
			ListVPS[idx].p = p
			fmt.Println("Received p: \n ", p)

		}
		listVPSMu.Unlock()

		// Xử lý metrics ở đây (ví dụ: lưu vào cơ sở dữ liệu, in ra console, v.v.)
		// Hiện tại chỉ in ra console
		fmt.Println("Received metrics: \n ", Metrix)

		c.JSON(200, gin.H{"status": "metrics received"})
	})

	router.Run(":8083")

}
