package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 支持：
//
//	@every 30s / @every 5m / @every 2h
//	标准 5 段 cron: 分 时 日 月 周（* , - / 以及单个数字）
type Schedule struct {
	Every time.Duration // >0 表示固定间隔
	Cron  *Cron
	Raw   string
}

func Parse(raw string) (*Schedule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("schedule 为空")
	}
	if strings.HasPrefix(raw, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(raw, "@every ")))
		if err != nil {
			return nil, fmt.Errorf("非法 @every: %w", err)
		}
		// 拒绝而非静默收敛：0s / 负间隔会造成每秒一次的 run 风暴
		if d <= 0 {
			return nil, fmt.Errorf("@every 间隔必须为正数，收到 %s", d)
		}
		if d < time.Second {
			return nil, fmt.Errorf("@every 间隔至少 1s，收到 %s", d)
		}
		if d > maxEvery {
			return nil, fmt.Errorf("@every 间隔至多 %s，收到 %s", maxEvery, d)
		}
		return &Schedule{Every: d, Raw: raw}, nil
	}
	c, err := parseCron(raw)
	if err != nil {
		return nil, err
	}
	// 不可满足的 cron（如 0 0 30 2 *，2 月 30 日）会让任务永不触发又占着调度位，直接拒
	if !c.Satisfiable(time.Now()) {
		return nil, fmt.Errorf("cron %q 在 4 年内没有任何可触发时间（日期组合不可能满足）", raw)
	}
	return &Schedule{Cron: c, Raw: raw}, nil
}

// maxEvery 是 @every 允许的最大间隔（1 年）
const maxEvery = 365 * 24 * time.Hour

// Next 返回 after 之后的下一次触发时间
func (s *Schedule) Next(after time.Time) time.Time {
	if s.Every > 0 {
		return after.Add(s.Every).Truncate(time.Second)
	}
	return s.Cron.Next(after)
}

type Cron struct {
	min, hour, dom, mon, dow map[int]bool
	domStar, dowStar         bool
}

func parseCron(expr string) (*Cron, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("cron 需 5 段（分 时 日 月 周），收到 %d 段", len(f))
	}
	c := &Cron{}
	var err error
	if c.min, err = parseField(f[0], 0, 59); err != nil {
		return nil, fmt.Errorf("分: %w", err)
	}
	if c.hour, err = parseField(f[1], 0, 23); err != nil {
		return nil, fmt.Errorf("时: %w", err)
	}
	if c.dom, err = parseField(f[2], 1, 31); err != nil {
		return nil, fmt.Errorf("日: %w", err)
	}
	if c.mon, err = parseField(f[3], 1, 12); err != nil {
		return nil, fmt.Errorf("月: %w", err)
	}
	if c.dow, err = parseField(f[4], 0, 6); err != nil {
		return nil, fmt.Errorf("周: %w", err)
	}
	c.domStar = f[2] == "*"
	c.dowStar = f[4] == "*"
	return c, nil
}

func parseField(field string, min, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("空字段")
		}
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			s, err := strconv.Atoi(part[i+1:])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("非法步长 %q", part)
			}
			step = s
			part = part[:i]
		}
		lo, hi := min, max
		switch {
		case part == "*" || part == "":
		case strings.Contains(part, "-"):
			ab := strings.SplitN(part, "-", 2)
			a, err1 := strconv.Atoi(ab[0])
			b, err2 := strconv.Atoi(ab[1])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("非法区间 %q", part)
			}
			lo, hi = a, b
		default:
			v, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("非法值 %q", part)
			}
			if strings.Contains(field, "/") {
				lo = v
			} else {
				lo, hi = v, v
			}
		}
		if lo < min {
			lo = min
		}
		if hi > max {
			hi = max
		}
		for v := lo; v <= hi; v += step {
			out[v] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("空集合")
	}
	return out, nil
}

// Satisfiable 报告该 cron 在 4 年内是否有可触发时间。
// Next 找不到的哨兵是 after+24h，故用"结果是否等于哨兵"来判定。
func (c *Cron) Satisfiable(after time.Time) bool {
	got := c.Next(after)
	return !got.Equal(after.Add(24 * time.Hour))
}

// Next 逐分钟向后找，最多看 4 年
func (c *Cron) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(4, 0, 0)
	for t.Before(limit) {
		if !c.mon[int(t.Month())] {
			// 跳到下个月 1 日 00:00
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}
		domOK := c.dom[t.Day()]
		dowOK := c.dow[int(t.Weekday())]
		var dayOK bool
		switch {
		case c.domStar && c.dowStar:
			dayOK = true
		case c.domStar:
			dayOK = dowOK
		case c.dowStar:
			dayOK = domOK
		default:
			dayOK = domOK || dowOK
		}
		if !dayOK {
			t = t.AddDate(0, 0, 1)
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			continue
		}
		if !c.hour[t.Hour()] {
			t = t.Add(time.Hour)
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
			continue
		}
		if !c.min[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return after.Add(24 * time.Hour)
}
