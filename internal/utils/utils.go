package utils

import (
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

func ParseExtranonceResult(res interface{}) (string, int, bool) {
	switch v := res.(type) {
	case []interface{}:
		if len(v) < 3 {
			return "", 0, false
		}
		ex1, ok1 := v[1].(string)
		ex2, ok2 := parseExtranonceSize(v[2])
		if !ok1 || !ok2 {
			return "", 0, false
		}
		return ex1, ex2, ex1 != "" && ex2 > 0
	case map[string]interface{}:
		ex1Raw, ok1 := v["extranonce1"]
		ex2Raw, ok2 := v["extranonce2_size"]
		if !ok1 || !ok2 {
			return "", 0, false
		}
		ex1, ok1 := ex1Raw.(string)
		ex2, ok2 := parseExtranonceSize(ex2Raw)
		if !ok1 || !ok2 {
			return "", 0, false
		}
		return ex1, ex2, ex1 != "" && ex2 > 0
	default:
		return "", 0, false
	}
}

func parseExtranonceSize(v interface{}) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), int(t) > 0
	case string:
		if t == "" {
			return 0, false
		}
		n, err := strconv.Atoi(t)
		if err != nil {
			return 0, false
		}
		return n, n > 0
	default:
		return 0, false
	}
}

func FmtDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	d = d.Round(time.Millisecond)
	return d.String()
}

func DiffFromBits(bits string) float64 {
	bits = strings.TrimPrefix(bits, "0x")
	if bits == "" {
		return 0
	}
	val, err := strconv.ParseUint(bits, 16, 32)
	if err != nil {
		return 0
	}
	exponent := byte(val >> 24)
	mantissa := val & 0xFFFFFF
	if mantissa == 0 || exponent <= 3 {
		return 0
	}
	target := new(big.Int).Lsh(big.NewInt(int64(mantissa)), uint(8*(int(exponent)-3)))
	if target.Sign() <= 0 {
		return 0
	}
	diffOne := new(big.Int).Lsh(big.NewInt(0xFFFF), uint(8*(0x1d-3)))
	t := new(big.Float).SetInt(target)
	d := new(big.Float).SetInt(diffOne)
	res := new(big.Float).Quo(d, t)
	out, _ := res.Float64()
	return out
}

func Backoff(min, max time.Duration) time.Duration {
	d := min
	if max > min {
		mul := 1 << (rand.Intn(4)) // 1,2,4,8
		d = time.Duration(int(min) * mul)
		if d > max {
			d = max
		}
	}
	return d + time.Duration(rand.Intn(250))*time.Millisecond
}

func CopyID(id *int64) *int64 {
	if id == nil {
		return nil
	}
	dup := new(int64)
	*dup = *id
	return dup
}
