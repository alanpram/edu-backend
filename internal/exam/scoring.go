// Package exam berisi logika pemilihan soal dan penilaian ujian yang murni
// (tanpa akses database) agar mudah diuji dan dipakai ulang oleh handler.
package exam

import (
	"math"
	"strings"
)

type SoalOption struct {
	Label     string   `json:"label"`
	Teks      string   `json:"teks"`
	GambarURL string   `json:"gambar_url,omitempty"`
	GambarAlt string   `json:"gambar_alt,omitempty"`
	IsCorrect bool     `json:"is_correct,omitempty"`
	Skor      *float64 `json:"skor,omitempty"`
}

type AnswerForScoring struct {
	SoalKategori string
	SoalTipe     string
	SoalOpsi     any
	SkorMaks     float64
	Jawaban      any
	SkorDidapat  *float64
}

type QuestionScore struct {
	Earned       float64
	Max          float64
	IsCorrect    *bool
	PendingEssay bool
	Selected     string
}

type CategoryBreakdown struct {
	Kategori           string   `json:"kategori"`
	Skor               float64  `json:"skor"`
	Bobot              float64  `json:"bobot"`
	PoinDidapat        float64  `json:"poin_didapat"`
	PoinMaks           float64  `json:"poin_maks"`
	JumlahSoal         int      `json:"jumlah_soal"`
	JumlahEsaiTertunda int      `json:"jumlah_esai_tertunda"`
	AmbangKategori     *float64 `json:"ambang_kategori"`
	LulusKategori      *bool    `json:"lulus_kategori"`
	SelisihKategori    *float64 `json:"selisih_kategori"`
}

type Breakdown struct {
	Categories         []CategoryBreakdown `json:"categories"`
	PendingEssay       int                 `json:"pending_essay"`
	TotalQuestions     int                 `json:"total_questions"`
	GradedQuestions    int                 `json:"graded_questions"`
	WeightsNormalized  bool                `json:"weights_normalized"`
	Method             string              `json:"method"`
	PassingMode        string              `json:"passing_mode"`
	SkalaNilai         string              `json:"skala_nilai"`
	PassingGrade       float64             `json:"passing_grade"`
	CategoryThresholds map[string]float64  `json:"category_thresholds"`
	FailedCategories   []string            `json:"failed_categories"`
	TotalPoinMaks      float64             `json:"total_poin_maks"`
	IsFinal            bool                `json:"is_final"`
}

type Result struct {
	FinalScore   float64   `json:"final_score"`
	IsPassed     bool      `json:"is_passed"`
	Breakdown    Breakdown `json:"score_breakdown"`
	PendingEssay int       `json:"pending_essay"`
}

func Round2(value float64) float64 {
	return math.Round((value+math.SmallestNonzeroFloat64)*100) / 100
}

func NormalizePassingMode(raw string) string {
	if raw == "per_category" {
		return "per_category"
	}
	return "overall"
}

func NormalizeFinalScoreMode(raw string) string {
	if raw == "poin_mentah" {
		return "poin_mentah"
	}
	return "skala_100"
}

// ParseOptions menormalkan JSON soal_opsi menjadi daftar opsi.
func ParseOptions(raw any) []SoalOption {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]SoalOption, 0, len(values))
	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		label := stringValue(item["label"])
		if label == "" {
			label = string(rune('A' + index))
		}
		teks := stringValue(item["teks"])
		if teks == "" {
			teks = stringValue(item["text"])
		}
		option := SoalOption{
			Label:     strings.ToUpper(strings.TrimSpace(label)),
			Teks:      strings.TrimSpace(teks),
			GambarURL: strings.TrimSpace(stringValue(item["gambar_url"])),
			GambarAlt: strings.TrimSpace(stringValue(item["gambar_alt"])),
		}
		if item["is_correct"] == true {
			option.IsCorrect = true
		}
		if score, exists := item["skor"]; exists {
			switch typed := score.(type) {
			case float64:
				if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
					option.Skor = &typed
				}
			case int:
				value := float64(typed)
				option.Skor = &value
			}
		}
		result = append(result, option)
	}
	return result
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

type normalizedAnswer struct {
	Label *string
	Index *int
}

func normalizeAnswer(raw any) normalizedAnswer {
	switch typed := raw.(type) {
	case string:
		label := strings.ToUpper(strings.TrimSpace(typed))
		return normalizedAnswer{Label: &label}
	case float64:
		if typed >= 0 && typed == math.Trunc(typed) {
			index := int(typed)
			return normalizedAnswer{Index: &index}
		}
	case map[string]any:
		candidate := typed["label"]
		if candidate == nil {
			candidate = typed["value"]
		}
		if candidate == nil {
			candidate = typed["opsi"]
		}
		if candidate == nil {
			candidate = typed["id"]
		}
		if label, ok := candidate.(string); ok {
			value := strings.ToUpper(strings.TrimSpace(label))
			return normalizedAnswer{Label: &value}
		}
		if index, ok := typed["index"].(float64); ok && index >= 0 && index == math.Trunc(index) {
			value := int(index)
			return normalizedAnswer{Index: &value}
		}
	}
	return normalizedAnswer{}
}

func findSelected(options []SoalOption, answer normalizedAnswer) *SoalOption {
	if answer.Label != nil {
		for index := range options {
			if strings.ToUpper(strings.TrimSpace(options[index].Label)) == *answer.Label {
				return &options[index]
			}
		}
		return nil
	}
	if answer.Index != nil && *answer.Index >= 0 && *answer.Index < len(options) {
		return &options[*answer.Index]
	}
	return nil
}

func ScoreQuestion(answer AnswerForScoring) QuestionScore {
	options := ParseOptions(answer.SoalOpsi)
	maxPoint := answer.SkorMaks
	if maxPoint < 0 || math.IsNaN(maxPoint) || math.IsInf(maxPoint, 0) {
		maxPoint = 1
	}
	if maxPoint == 0 {
		maxPoint = 1
	}
	normalized := normalizeAnswer(answer.Jawaban)
	selected := findSelected(options, normalized)

	if answer.SoalTipe == "essay" {
		if answer.SkorDidapat == nil {
			return QuestionScore{Earned: 0, Max: maxPoint, PendingEssay: true}
		}
		earned := math.Min(math.Max(0, *answer.SkorDidapat), maxPoint)
		correct := earned >= maxPoint
		return QuestionScore{Earned: earned, Max: maxPoint, IsCorrect: &correct}
	}

	if answer.SoalTipe == "gradasi" {
		optionMax := 0.0
		for _, option := range options {
			if option.Skor != nil && *option.Skor > optionMax {
				optionMax = *option.Skor
			}
		}
		effectiveMax := optionMax
		if effectiveMax <= 0 {
			effectiveMax = maxPoint
		}
		earned := 0.0
		label := ""
		if selected != nil {
			if selected.Skor != nil {
				earned = *selected.Skor
			}
			label = selected.Label
		}
		earned = math.Min(math.Max(0, earned), effectiveMax)
		correct := effectiveMax > 0 && earned >= effectiveMax
		return QuestionScore{Earned: earned, Max: effectiveMax, IsCorrect: &correct, Selected: label}
	}

	var correctOption *SoalOption
	for index := range options {
		if options[index].IsCorrect {
			correctOption = &options[index]
			break
		}
	}
	correct := false
	label := ""
	if selected != nil {
		label = selected.Label
		if correctOption != nil && strings.EqualFold(strings.TrimSpace(correctOption.Label), strings.TrimSpace(selected.Label)) {
			correct = true
		}
	}
	earned := 0.0
	if correct {
		earned = maxPoint
	}
	return QuestionScore{Earned: earned, Max: maxPoint, IsCorrect: &correct, Selected: label}
}

func QuestionMaxScore(tipe string, options any, skorMaks float64) float64 {
	maxPoint := skorMaks
	if maxPoint <= 0 {
		maxPoint = 1
	}
	if tipe != "gradasi" {
		return maxPoint
	}
	optionMax := 0.0
	for _, option := range ParseOptions(options) {
		if option.Skor != nil && *option.Skor > optionMax {
			optionMax = *option.Skor
		}
	}
	if optionMax > 0 {
		return optionMax
	}
	return maxPoint
}

func ParseWeights(raw any) map[string]float64 {
	result := map[string]float64{}
	items, ok := raw.(map[string]any)
	if !ok {
		return result
	}
	for key, value := range items {
		number, ok := toFloat(value)
		if ok && number > 0 {
			result[strings.TrimSpace(key)] = number
		}
	}
	return result
}

func ParseThresholds(raw any) map[string]float64 {
	result := map[string]float64{}
	items, ok := raw.(map[string]any)
	if !ok {
		return result
	}
	for key, value := range items {
		number, ok := toFloat(value)
		if ok && number >= 0 && strings.TrimSpace(key) != "" {
			result[strings.TrimSpace(key)] = number
		}
	}
	return result
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case int:
		return float64(typed), true
	}
	return 0, false
}

type CalculateInput struct {
	Answers            []AnswerForScoring
	Weights            any
	PassingMode        string
	PassingGrade       float64
	CategoryThresholds any
	FinalScoreMode     string
}

type categoryAccumulator struct {
	earned       float64
	max          float64
	count        int
	essayPending int
}

func CalculateFinalScore(input CalculateInput) Result {
	passingMode := NormalizePassingMode(input.PassingMode)
	finalScoreMode := NormalizeFinalScoreMode(input.FinalScoreMode)
	categories := map[string]*categoryAccumulator{}
	order := []string{}
	pendingEssay := 0
	gradedQuestions := 0

	for _, answer := range input.Answers {
		category := strings.TrimSpace(answer.SoalKategori)
		if category == "" {
			category = "UMUM"
		}
		result := ScoreQuestion(answer)
		accumulator, exists := categories[category]
		if !exists {
			accumulator = &categoryAccumulator{}
			categories[category] = accumulator
			order = append(order, category)
		}
		accumulator.earned += result.Earned
		accumulator.max += result.Max
		accumulator.count++
		if result.PendingEssay {
			accumulator.essayPending++
			pendingEssay++
		} else {
			gradedQuestions++
		}
	}

	configuredWeights := ParseWeights(input.Weights)
	thresholds := ParseThresholds(input.CategoryThresholds)
	totalConfiguredWeight := 0.0
	for _, category := range order {
		totalConfiguredWeight += configuredWeights[category]
	}
	useConfigured := totalConfiguredWeight > 0
	equalWeight := 0.0
	if len(order) > 0 {
		equalWeight = 100 / float64(len(order))
	}

	breakdown := Breakdown{
		Categories:         make([]CategoryBreakdown, 0, len(order)),
		PendingEssay:       pendingEssay,
		TotalQuestions:     len(input.Answers),
		GradedQuestions:    gradedQuestions,
		WeightsNormalized:  useConfigured,
		PassingMode:        passingMode,
		SkalaNilai:         finalScoreMode,
		PassingGrade:       Round2(input.PassingGrade),
		CategoryThresholds: thresholds,
		FailedCategories:   []string{},
	}

	for _, category := range order {
		accumulator := categories[category]
		rawScore := 0.0
		if accumulator.max > 0 {
			rawScore = accumulator.earned / accumulator.max * 100
		}
		weight := equalWeight
		if useConfigured {
			weight = configuredWeights[category] / totalConfiguredWeight * 100
		}
		var threshold *float64
		var passed *bool
		var difference *float64
		if value, exists := thresholds[category]; exists {
			thresholdValue := value
			threshold = &thresholdValue
			passedValue := Round2(accumulator.earned) >= value
			passed = &passedValue
			diffValue := Round2(accumulator.earned - value)
			difference = &diffValue
			if !passedValue {
				breakdown.FailedCategories = append(breakdown.FailedCategories, category)
			}
		}
		breakdown.Categories = append(breakdown.Categories, CategoryBreakdown{
			Kategori:           category,
			Skor:               Round2(rawScore),
			Bobot:              Round2(weight),
			PoinDidapat:        Round2(accumulator.earned),
			PoinMaks:           Round2(accumulator.max),
			JumlahSoal:         accumulator.count,
			JumlahEsaiTertunda: accumulator.essayPending,
			AmbangKategori:     threshold,
			LulusKategori:      passed,
			SelisihKategori:    difference,
		})
	}

	weightedScore := 0.0
	totalRaw := 0.0
	totalMax := 0.0
	for _, category := range breakdown.Categories {
		weightedScore += category.Skor * category.Bobot / 100
		totalRaw += category.PoinDidapat
		totalMax += category.PoinMaks
	}
	finalScore := weightedScore
	if finalScoreMode == "poin_mentah" {
		finalScore = totalRaw
	}
	finalScore = Round2(finalScore)
	breakdown.TotalPoinMaks = Round2(totalMax)
	breakdown.IsFinal = pendingEssay == 0

	missingThresholds := []string{}
	if passingMode == "per_category" {
		for _, category := range order {
			if _, exists := thresholds[category]; !exists {
				missingThresholds = append(missingThresholds, category)
			}
		}
	}

	isPassed := false
	if passingMode == "per_category" {
		isPassed = len(order) > 0 && len(thresholds) > 0 && len(missingThresholds) == 0 && len(breakdown.FailedCategories) == 0
	} else {
		isPassed = finalScore >= input.PassingGrade
	}

	if passingMode == "per_category" {
		breakdown.Method = "per_category_raw_points"
	} else if finalScoreMode == "poin_mentah" {
		breakdown.Method = "raw_points_total"
	} else {
		breakdown.Method = "weighted_category_scale_100"
	}

	return Result{FinalScore: finalScore, IsPassed: isPassed, Breakdown: breakdown, PendingEssay: pendingEssay}
}
