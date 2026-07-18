package dpi

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type MockDetector struct {
	BaseDetector
	ticker   *time.Ticker
	stopChan chan struct{}
	mu       sync.Mutex
}

func NewMockDetector() *MockDetector {
	return &MockDetector{
		BaseDetector: *NewBaseDetector(),
	}
}

func (d *MockDetector) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil
	}

	d.running = true
	d.stopChan = make(chan struct{})
	d.ticker = time.NewTicker(2 * time.Second)

	go d.generateMockFlows()

	return nil
}

func (d *MockDetector) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	d.running = false
	d.ticker.Stop()
	close(d.stopChan)

	return nil
}

func (d *MockDetector) DetectPacket(packet []byte) (*FlowInfo, error) {
	return nil, fmt.Errorf("mock detector does not support real packet detection")
}

func (d *MockDetector) generateMockFlows() {
	clientIPs := []string{
		"192.168.1.70",
		"192.168.1.71",
		"192.168.1.72",
		"192.168.1.73",
		"192.168.1.74",
	}

	apps := []string{
		"https", "http", "dns", "wechat", "douyin",
		"bilibili", "qq", "steam", "taobao", "zhihu",
		"weibo", "aliyun", "thunder", "youtube", "netflix",
	}

	ports := map[string]uint16{
		"https": 443,
		"http": 80,
		"dns": 53,
		"wechat": 80,
		"douyin": 443,
		"bilibili": 443,
		"qq": 8000,
		"steam": 27015,
		"taobao": 443,
		"zhihu": 443,
		"weibo": 443,
		"aliyun": 443,
		"thunder": 9000,
		"youtube": 443,
		"netflix": 443,
	}

	for {
		select {
		case <-d.stopChan:
			return
		case <-d.ticker.C:
			for i := 0; i < 3; i++ {
				app := apps[rand.Intn(len(apps))]
				clientIP := clientIPs[rand.Intn(len(clientIPs))]
				flowID := d.nextFlowID()

				flow := &FlowInfo{
					ID:          flowID,
					SrcIP:       clientIP,
					DstIP:       fmt.Sprintf("%d.%d.%d.%d", rand.Intn(223)+1, rand.Intn(255), rand.Intn(255), rand.Intn(254)+1),
					SrcPort:     uint16(rand.Intn(64511) + 1024),
					DstPort:     ports[app],
					Protocol:    "TCP",
					Application: app,
					Detected:    true,
					DetectedAt:  time.Now(),
					Packets:     rand.Intn(100) + 10,
					Bytes:       rand.Intn(100000) + 1000,
				}

				d.mu.Lock()
				d.flows[flowID] = flow
				d.mu.Unlock()

				d.notifyCallbacks(flow)
			}

			d.mu.Lock()
			if len(d.flows) > 50 {
				i := 0
				for id := range d.flows {
					delete(d.flows, id)
					i++
					if i >= 20 {
						break
					}
				}
			}
			d.mu.Unlock()
		}
	}
}
