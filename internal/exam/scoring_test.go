package exam

import "testing"

func options() []any {
	return []any{
		map[string]any{"label": "A", "teks": "Salah", "is_correct": false},
		map[string]any{"label": "B", "teks": "Benar", "is_correct": true},
	}
}

func TestScoreQuestionExact(t *testing.T) {
	answer := AnswerForScoring{
		SoalKategori: "UMUM",
		SoalTipe:     "exact",
		SoalOpsi:     options(),
		SkorMaks:     2,
		Jawaban:      "b",
	}
	result := ScoreQuestion(answer)
	if result.Earned != 2 || result.Max != 2 {
		t.Fatalf("expected 2/2, got %v/%v", result.Earned, result.Max)
	}
	if result.IsCorrect == nil || !*result.IsCorrect {
		t.Fatal("expected exact answer to be correct")
	}
	if result.Selected != "B" {
		t.Fatalf("expected selected label B, got %q", result.Selected)
	}

	answer.Jawaban = "A"
	result = ScoreQuestion(answer)
	if result.Earned != 0 {
		t.Fatalf("expected 0 for wrong answer, got %v", result.Earned)
	}
	if result.IsCorrect == nil || *result.IsCorrect {
		t.Fatal("expected wrong answer to be incorrect")
	}
}

func TestScoreQuestionGradasiUsesOptionMaximum(t *testing.T) {
	answer := AnswerForScoring{
		SoalKategori: "UMUM",
		SoalTipe:     "gradasi",
		SoalOpsi: []any{
			map[string]any{"label": "A", "teks": "Kurang", "skor": 1.0},
			map[string]any{"label": "B", "teks": "Cukup", "skor": 3.0},
		},
		SkorMaks: 10,
		Jawaban:  "B",
	}
	result := ScoreQuestion(answer)
	if result.Earned != 3 || result.Max != 3 {
		t.Fatalf("expected 3/3 based on option maximum, got %v/%v", result.Earned, result.Max)
	}
}

func TestScoreQuestionEssayPendingAndGraded(t *testing.T) {
	answer := AnswerForScoring{SoalKategori: "ESAI", SoalTipe: "essay", SkorMaks: 10}
	pending := ScoreQuestion(answer)
	if !pending.PendingEssay || pending.Earned != 0 {
		t.Fatalf("expected pending essay, got %+v", pending)
	}
	score := 7.5
	answer.SkorDidapat = &score
	graded := ScoreQuestion(answer)
	if graded.PendingEssay || graded.Earned != 7.5 || graded.Max != 10 {
		t.Fatalf("expected graded essay 7.5/10, got %+v", graded)
	}
}

func TestCalculateFinalScoreWeightedOverall(t *testing.T) {
	correct := []any{map[string]any{"label": "A", "is_correct": true}}
	input := CalculateInput{
		Answers: []AnswerForScoring{
			{SoalKategori: "TWK", SoalTipe: "exact", SoalOpsi: correct, SkorMaks: 1, Jawaban: "A"},
			{SoalKategori: "TIU", SoalTipe: "exact", SoalOpsi: correct, SkorMaks: 1, Jawaban: "B"},
		},
		Weights:      map[string]any{"TWK": 60.0, "TIU": 40.0},
		PassingMode:  "overall",
		PassingGrade: 65,
	}
	result := CalculateFinalScore(input)
	if result.FinalScore != 60 {
		t.Fatalf("expected weighted score 60, got %v", result.FinalScore)
	}
	if result.IsPassed {
		t.Fatal("expected not passed with score 60 < 65")
	}
	if result.Breakdown.Method != "weighted_category_scale_100" {
		t.Fatalf("unexpected method %q", result.Breakdown.Method)
	}
	if len(result.Breakdown.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(result.Breakdown.Categories))
	}
}

func TestCalculateFinalScoreRawPointsAndDefaultZeroPassingGrade(t *testing.T) {
	correct := []any{map[string]any{"label": "A", "is_correct": true}}
	input := CalculateInput{
		Answers: []AnswerForScoring{
			{SoalKategori: "UMUM", SoalTipe: "exact", SoalOpsi: correct, SkorMaks: 5, Jawaban: "A"},
			{SoalKategori: "UMUM", SoalTipe: "exact", SoalOpsi: correct, SkorMaks: 5, Jawaban: "B"},
		},
		FinalScoreMode: "poin_mentah",
		PassingMode:    "overall",
		PassingGrade:   0,
	}
	result := CalculateFinalScore(input)
	if result.FinalScore != 5 {
		t.Fatalf("expected raw point score 5, got %v", result.FinalScore)
	}
	if !result.IsPassed {
		t.Fatal("expected passing grade 0 to pass a non-negative score")
	}
	if result.Breakdown.Method != "raw_points_total" {
		t.Fatalf("unexpected method %q", result.Breakdown.Method)
	}
}

func TestCalculateFinalScorePerCategory(t *testing.T) {
	correct := []any{map[string]any{"label": "A", "is_correct": true}}
	input := CalculateInput{
		Answers: []AnswerForScoring{
			{SoalKategori: "TWK", SoalTipe: "exact", SoalOpsi: correct, SkorMaks: 3, Jawaban: "A"},
			{SoalKategori: "TIU", SoalTipe: "exact", SoalOpsi: correct, SkorMaks: 3, Jawaban: "B"},
		},
		PassingMode:        "per_category",
		PassingGrade:       65,
		CategoryThresholds: map[string]any{"TWK": 2.0, "TIU": 2.0},
	}
	result := CalculateFinalScore(input)
	if result.IsPassed {
		t.Fatal("expected per-category failure because TIU earned 0")
	}
	if len(result.Breakdown.FailedCategories) != 1 || result.Breakdown.FailedCategories[0] != "TIU" {
		t.Fatalf("expected TIU to fail, got %+v", result.Breakdown.FailedCategories)
	}

	input.Answers[1].Jawaban = "A"
	result = CalculateFinalScore(input)
	if !result.IsPassed {
		t.Fatalf("expected both categories to pass, got %+v", result.Breakdown.FailedCategories)
	}
}
