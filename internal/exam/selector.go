package exam

import (
	"crypto/rand"
	"math"
	"math/big"
	"strings"
)

type QuestionMeta struct {
	SoalID        int
	SoalKategori  string
	SoalKesulitan string
}

type SelectionResult struct {
	IDs       []int
	Requested int
	Selected  int
	Warnings  []string
}

var difficulties = []string{"mudah", "sedang", "sulit"}

func shuffle[T any](items []T) []T {
	result := append([]T(nil), items...)
	for i := len(result) - 1; i > 0; i-- {
		limit := big.NewInt(int64(i + 1))
		value, err := rand.Int(rand.Reader, limit)
		if err != nil {
			panic(err)
		}
		j := int(value.Int64())
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func hashSeed(seed string) uint32 {
	var hash uint32 = 2166136261
	for _, char := range seed {
		hash ^= uint32(char)
		hash *= 16777619
	}
	return hash
}

func mulberry32(seed uint32) func() float64 {
	state := seed
	return func() float64 {
		state += 0x6d2b79f5
		value := state
		value = (value ^ (value >> 15)) * (value | 1)
		value ^= value + (value^(value>>7))*(value|61)
		return float64(value^(value>>14)) / 4294967296.0
	}
}

func seededShuffle[T any](items []T, seed string) []T {
	result := append([]T(nil), items...)
	random := mulberry32(hashSeed(seed))
	for i := len(result) - 1; i > 0; i-- {
		j := int(math.Floor(random() * float64(i+1)))
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func normalizeDifficulty(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, difficulty := range difficulties {
		if normalized == difficulty {
			return normalized
		}
	}
	return ""
}

type bucketMap struct {
	byCategoryDifficulty map[string][]QuestionMeta
	byDifficulty         map[string][]QuestionMeta
	byCategory           map[string][]QuestionMeta
	all                  []QuestionMeta
	categoryNames        map[string]string
}

func buildBuckets(questions []QuestionMeta) bucketMap {
	buckets := bucketMap{
		byCategoryDifficulty: map[string][]QuestionMeta{},
		byDifficulty:         map[string][]QuestionMeta{},
		byCategory:           map[string][]QuestionMeta{},
		all:                  questions,
		categoryNames:        map[string]string{},
	}
	for _, question := range questions {
		difficulty := normalizeDifficulty(question.SoalKesulitan)
		category := strings.TrimSpace(question.SoalKategori)
		key := strings.ToLower(category)
		buckets.categoryNames[key] = category
		if difficulty != "" {
			buckets.byCategoryDifficulty[key+"::"+difficulty] = append(buckets.byCategoryDifficulty[key+"::"+difficulty], question)
			buckets.byDifficulty[difficulty] = append(buckets.byDifficulty[difficulty], question)
		}
		buckets.byCategory[key] = append(buckets.byCategory[key], question)
	}
	return buckets
}

func parseComposition(raw any) (map[string]map[string]int, bool) {
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	result := map[string]map[string]int{}
	hasValues := false
	for category, rawRules := range items {
		rules, ok := rawRules.(map[string]any)
		if !ok {
			continue
		}
		parsed := map[string]int{}
		for _, difficulty := range difficulties {
			value, exists := rules[difficulty]
			if !exists {
				continue
			}
			number := 0
			switch typed := value.(type) {
			case float64:
				if typed < 0 || typed != math.Trunc(typed) {
					return nil, false
				}
				number = int(typed)
			case int:
				if typed < 0 {
					return nil, false
				}
				number = typed
			default:
				return nil, false
			}
			parsed[difficulty] = number
			if number > 0 {
				hasValues = true
			}
		}
		result[category] = parsed
	}
	return result, hasValues
}

func pickFromComposition(buckets bucketMap, composition map[string]map[string]int, shuffleFn func([]QuestionMeta) []QuestionMeta) []int {
	selected := []int{}
	used := map[int]bool{}
	for category, rules := range composition {
		actualCategory, ok := buckets.categoryNames[strings.ToLower(strings.TrimSpace(category))]
		if !ok {
			return nil
		}
		for _, difficulty := range difficulties {
			count := rules[difficulty]
			if count <= 0 {
				continue
			}
			available := []QuestionMeta{}
			for _, question := range buckets.byCategoryDifficulty[strings.ToLower(actualCategory)+"::"+difficulty] {
				if !used[question.SoalID] {
					available = append(available, question)
				}
			}
			if len(available) < count {
				return nil
			}
			for _, question := range shuffleFn(available)[:count] {
				used[question.SoalID] = true
				selected = append(selected, question.SoalID)
			}
		}
	}
	return selected
}

func pickBalanced(buckets bucketMap, targetCount int, shuffleFn func([]QuestionMeta) []QuestionMeta) ([]int, []string) {
	selected := []int{}
	used := map[int]bool{}
	warnings := []string{}
	target := targetCount
	if target > len(buckets.all) {
		target = len(buckets.all)
	}
	if targetCount > len(buckets.all) {
		warnings = append(warnings, "Jumlah soal yang diminta melebihi bank soal aktif; seluruh soal tersedia dipakai.")
	}

	ratios := map[string]float64{"mudah": 0.3, "sedang": 0.5, "sulit": 0.2}
	carry := 0
	for _, difficulty := range difficulties {
		quota := int(math.Round(float64(target)*ratios[difficulty])) + carry
		pool := []QuestionMeta{}
		for _, question := range buckets.byDifficulty[difficulty] {
			if !used[question.SoalID] {
				pool = append(pool, question)
			}
		}
		if len(pool) < quota {
			carry = quota - len(pool)
			quota = len(pool)
		} else {
			carry = 0
		}
		if quota <= 0 {
			continue
		}
		groups := map[string][]QuestionMeta{}
		for _, question := range shuffleFn(pool) {
			key := strings.ToLower(question.SoalKategori)
			groups[key] = append(groups[key], question)
		}
		queue := [][]QuestionMeta{}
		for _, group := range groups {
			queue = append(queue, group)
		}
		queue = shuffleFn2(queue, shuffleFn)
		remaining := quota
		for remaining > 0 {
			picked := false
			for index := range queue {
				if remaining <= 0 {
					break
				}
				group := queue[index]
				if len(group) == 0 {
					continue
				}
				next := group[len(group)-1]
				queue[index] = group[:len(group)-1]
				if !used[next.SoalID] {
					used[next.SoalID] = true
					selected = append(selected, next.SoalID)
					remaining--
					picked = true
				}
			}
			if !picked {
				break
			}
		}
		carry += remaining
	}

	if len(selected) < target {
		remainingQuestions := []QuestionMeta{}
		for _, question := range buckets.all {
			if !used[question.SoalID] {
				remainingQuestions = append(remainingQuestions, question)
			}
		}
		for _, question := range shuffleFn(remainingQuestions) {
			if len(selected) >= target {
				break
			}
			used[question.SoalID] = true
			selected = append(selected, question.SoalID)
		}
	}
	return selected, warnings
}

// shuffleFn2 memakai fungsi shuffle acak/deterministik yang sama untuk
// mengacak antrean kategori tanpa menduplikasi logika.
func shuffleFn2(groups [][]QuestionMeta, shuffleFn func([]QuestionMeta) []QuestionMeta) [][]QuestionMeta {
	if len(groups) <= 1 {
		return groups
	}
	proxies := make([]QuestionMeta, len(groups))
	for index, group := range groups {
		if len(group) > 0 {
			proxies[index] = group[0]
		}
	}
	shuffled := shuffleFn(proxies)
	result := make([][]QuestionMeta, 0, len(groups))
	usedIndex := map[int]bool{}
	for _, proxy := range shuffled {
		for index, group := range groups {
			if !usedIndex[index] && len(group) > 0 && group[0].SoalID == proxy.SoalID {
				result = append(result, group)
				usedIndex[index] = true
				break
			}
		}
	}
	for index, group := range groups {
		if !usedIndex[index] {
			result = append(result, group)
		}
	}
	return result
}

func SelectQuestions(questions []QuestionMeta, composition any, targetCount int, mode string, seed string) SelectionResult {
	if len(questions) == 0 {
		return SelectionResult{}
	}
	buckets := buildBuckets(questions)
	parsed, hasComposition := parseComposition(composition)
	shuffleFn := shuffle[QuestionMeta]
	if mode == "tetap" {
		shuffleFn = func(items []QuestionMeta) []QuestionMeta { return seededShuffle(items, seed) }
	}

	if hasComposition {
		picked := pickFromComposition(buckets, parsed, shuffleFn)
		if len(picked) == 0 {
			return SelectionResult{}
		}
		if mode == "acak" {
			picked = shuffle(picked)
		}
		return SelectionResult{IDs: picked, Requested: len(picked), Selected: len(picked)}
	}

	target := targetCount
	if target < 1 {
		target = 1
	}
	ids, warnings := pickBalanced(buckets, target, shuffleFn)
	if len(ids) == 0 {
		return SelectionResult{}
	}
	if mode == "acak" {
		ids = shuffle(ids)
	}
	return SelectionResult{IDs: ids, Requested: target, Selected: len(ids), Warnings: warnings}
}
