package idgen

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	epoch        int64 = 1704067200000 // 2024-01-01 UTC
	machineBits        = 10
	seqBits            = 12
	machineMask        = (1 << machineBits) - 1
	seqMask            = (1 << seqBits) - 1
)

// Snowflake 本地发号，避免订单主键走数据库序列成为写入热点。
type Snowflake struct {
	mu        sync.Mutex
	machineID int64
	lastMs    int64
	seq       int64
}

func NewSnowflake() *Snowflake {
	mid := int64(1)
	if v := os.Getenv("SNOWFLAKE_MACHINE_ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			mid = n
		}
	}
	return &Snowflake{machineID: mid & machineMask}
}

func (s *Snowflake) next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	if now < s.lastMs {
		now = s.lastMs
	}
	if now == s.lastMs {
		s.seq = (s.seq + 1) & seqMask
		if s.seq == 0 {
			for now <= s.lastMs {
				now = time.Now().UnixMilli()
			}
			s.lastMs = now
		}
	} else {
		s.seq = 0
		s.lastMs = now
	}
	return ((now - epoch) << (machineBits + seqBits)) | (s.machineID << seqBits) | s.seq
}

func (s *Snowflake) OrderID() string  { return fmt.Sprintf("ord_%d", s.next()) }
func (s *Snowflake) EventID() string  { return fmt.Sprintf("evt_%d", s.next()) }
func (s *Snowflake) RefundID() string { return fmt.Sprintf("rfd_%d", s.next()) }
func (s *Snowflake) IntentID() string { return fmt.Sprintf("pi_%d", s.next()) }
