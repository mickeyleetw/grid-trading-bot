package grid

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestCalculator() *Calculator {
	return NewCalculator(zap.NewNop())
}

func TestCalculateGridLevels(t *testing.T) {
	calc := newTestCalculator()

	tests := []struct {
		name            string
		params          GridParams
		expectedCount   int
		expectedFirst   decimal.Decimal
		expectedLast    decimal.Decimal
		expectedSpacing decimal.Decimal
		expectError     bool
	}{
		{
			name: "10 grids from 40000 to 50000",
			params: GridParams{
				GridCount:  10,
				LowerBound: decimal.NewFromInt(40000),
				UpperBound: decimal.NewFromInt(50000),
			},
			expectedCount:   11, // 10 intervals = 11 levels
			expectedFirst:   decimal.NewFromInt(40000),
			expectedLast:    decimal.NewFromInt(50000),
			expectedSpacing: decimal.NewFromInt(1000),
			expectError:     false,
		},
		{
			name: "5 grids from 100 to 200",
			params: GridParams{
				GridCount:  5,
				LowerBound: decimal.NewFromInt(100),
				UpperBound: decimal.NewFromInt(200),
			},
			expectedCount:   6,
			expectedFirst:   decimal.NewFromInt(100),
			expectedLast:    decimal.NewFromInt(200),
			expectedSpacing: decimal.NewFromInt(20),
			expectError:     false,
		},
		{
			name: "invalid - zero grid count",
			params: GridParams{
				GridCount:  0,
				LowerBound: decimal.NewFromInt(100),
				UpperBound: decimal.NewFromInt(200),
			},
			expectError: true,
		},
		{
			name: "invalid - upper less than lower",
			params: GridParams{
				GridCount:  10,
				LowerBound: decimal.NewFromInt(200),
				UpperBound: decimal.NewFromInt(100),
			},
			expectError: true,
		},
		{
			name: "invalid - negative lower bound",
			params: GridParams{
				GridCount:  10,
				LowerBound: decimal.NewFromInt(-100),
				UpperBound: decimal.NewFromInt(100),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			levels, err := calc.CalculateGridLevels(tt.params)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, levels, tt.expectedCount)
			assert.True(t, levels[0].Price.Equal(tt.expectedFirst),
				"first level: expected %s, got %s", tt.expectedFirst, levels[0].Price)
			assert.True(t, levels[len(levels)-1].Price.Equal(tt.expectedLast),
				"last level: expected %s, got %s", tt.expectedLast, levels[len(levels)-1].Price)

			// Check spacing
			if len(levels) > 1 {
				spacing := levels[1].Price.Sub(levels[0].Price)
				assert.True(t, spacing.Equal(tt.expectedSpacing),
					"spacing: expected %s, got %s", tt.expectedSpacing, spacing)
			}

			// Check all levels have correct index
			for i, level := range levels {
				assert.Equal(t, i, level.Index)
				assert.Equal(t, LevelEmpty, level.Status)
			}
		})
	}
}

func TestCalculateOrderSize(t *testing.T) {
	calc := newTestCalculator()

	tests := []struct {
		name         string
		params       GridParams
		currentPrice decimal.Decimal
		expected     decimal.Decimal
	}{
		{
			name: "basic calculation with leverage 10",
			params: GridParams{
				TotalInvestment: decimal.NewFromInt(10000),
				GridCount:       10,
				Leverage:        10,
			},
			currentPrice: decimal.NewFromInt(2000),
			// 10000 / 10 = 1000 per grid
			// 1000 * 10 / 2000 = 5
			expected: decimal.NewFromInt(5),
		},
		{
			name: "with leverage 1",
			params: GridParams{
				TotalInvestment: decimal.NewFromInt(10000),
				GridCount:       10,
				Leverage:        1,
			},
			currentPrice: decimal.NewFromInt(2000),
			// 10000 / 10 = 1000 per grid
			// 1000 * 1 / 2000 = 0.5
			expected: decimal.NewFromFloat(0.5),
		},
		{
			name: "zero price returns zero",
			params: GridParams{
				TotalInvestment: decimal.NewFromInt(10000),
				GridCount:       10,
				Leverage:        10,
			},
			currentPrice: decimal.Zero,
			expected:     decimal.Zero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.CalculateOrderSize(tt.params, tt.currentPrice)
			assert.True(t, result.Equal(tt.expected),
				"expected %s, got %s", tt.expected, result)
		})
	}
}

func TestFindGridIndex(t *testing.T) {
	calc := newTestCalculator()

	// Create test levels: [100, 120, 140, 160, 180, 200]
	levels := []*GridLevel{
		{Index: 0, Price: decimal.NewFromInt(100)},
		{Index: 1, Price: decimal.NewFromInt(120)},
		{Index: 2, Price: decimal.NewFromInt(140)},
		{Index: 3, Price: decimal.NewFromInt(160)},
		{Index: 4, Price: decimal.NewFromInt(180)},
		{Index: 5, Price: decimal.NewFromInt(200)},
	}

	tests := []struct {
		name     string
		price    decimal.Decimal
		expected int
	}{
		{"at lowest level", decimal.NewFromInt(100), 0},
		{"at highest level", decimal.NewFromInt(200), 5},
		{"between levels", decimal.NewFromInt(130), 1},
		{"exactly at level", decimal.NewFromInt(140), 2},
		{"below lowest", decimal.NewFromInt(50), -1},
		{"just above lowest", decimal.NewFromInt(101), 0},
		{"just below highest", decimal.NewFromInt(199), 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.FindGridIndex(levels, tt.price)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindNearestLevelIndex(t *testing.T) {
	calc := newTestCalculator()

	levels := []*GridLevel{
		{Index: 0, Price: decimal.NewFromInt(100)},
		{Index: 1, Price: decimal.NewFromInt(120)},
		{Index: 2, Price: decimal.NewFromInt(140)},
	}

	tests := []struct {
		name     string
		price    decimal.Decimal
		expected int
	}{
		{"exactly at level 0", decimal.NewFromInt(100), 0},
		{"exactly at level 1", decimal.NewFromInt(120), 1},
		{"closer to level 0", decimal.NewFromInt(105), 0},
		{"closer to level 1", decimal.NewFromInt(115), 1},
		{"midpoint - closer to lower", decimal.NewFromInt(110), 0}, // ties go to lower
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.FindNearestLevelIndex(levels, tt.price)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNeedRebuild(t *testing.T) {
	calc := newTestCalculator()

	levels := []*GridLevel{
		{Index: 0, Price: decimal.NewFromInt(100)},
		{Index: 1, Price: decimal.NewFromInt(150)},
		{Index: 2, Price: decimal.NewFromInt(200)},
	}

	tests := []struct {
		name     string
		price    decimal.Decimal
		expected bool
	}{
		{"within range", decimal.NewFromInt(150), false},
		{"at lower bound", decimal.NewFromInt(100), false},
		{"at upper bound", decimal.NewFromInt(200), false},
		{"below lower bound", decimal.NewFromInt(99), true},
		{"above upper bound", decimal.NewFromInt(201), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.NeedRebuild(levels, tt.price)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRebuildGrid(t *testing.T) {
	calc := newTestCalculator()

	params := GridParams{
		InstID:     "ETH-USDT-SWAP",
		GridCount:  10,
		LowerBound: decimal.NewFromInt(40000),
		UpperBound: decimal.NewFromInt(50000),
	}

	t.Run("rebuild centered on new price", func(t *testing.T) {
		newPrice := decimal.NewFromInt(55000)
		levels, newParams, err := calc.RebuildGrid(params, newPrice)

		require.NoError(t, err)
		assert.Len(t, levels, 11)

		// New grid should be centered on 55000
		// Original range = 10000, half = 5000
		// New lower = 55000 - 5000 = 50000
		// New upper = 55000 + 5000 = 60000
		assert.True(t, newParams.LowerBound.Equal(decimal.NewFromInt(50000)))
		assert.True(t, newParams.UpperBound.Equal(decimal.NewFromInt(60000)))
	})

	t.Run("error on zero price", func(t *testing.T) {
		_, _, err := calc.RebuildGrid(params, decimal.Zero)
		assert.Error(t, err)
	})
}

func TestGetOppositeLevel(t *testing.T) {
	calc := newTestCalculator()
	totalLevels := 11 // 0-10

	tests := []struct {
		name        string
		filledIndex int
		side        OrderSide
		expected    int
	}{
		{"buy at 0 -> sell at 1", 0, OrderSideBuy, 1},
		{"buy at 5 -> sell at 6", 5, OrderSideBuy, 6},
		{"buy at 10 (max) -> no opposite", 10, OrderSideBuy, -1},
		{"sell at 10 -> buy at 9", 10, OrderSideSell, 9},
		{"sell at 5 -> buy at 4", 5, OrderSideSell, 4},
		{"sell at 0 (min) -> no opposite", 0, OrderSideSell, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.GetOppositeLevel(tt.filledIndex, tt.side, totalLevels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetGridSpacing(t *testing.T) {
	calc := newTestCalculator()

	t.Run("normal spacing", func(t *testing.T) {
		levels := []*GridLevel{
			{Price: decimal.NewFromInt(100)},
			{Price: decimal.NewFromInt(110)},
			{Price: decimal.NewFromInt(120)},
		}
		spacing := calc.GetGridSpacing(levels)
		assert.True(t, spacing.Equal(decimal.NewFromInt(10)))
	})

	t.Run("empty levels", func(t *testing.T) {
		spacing := calc.GetGridSpacing([]*GridLevel{})
		assert.True(t, spacing.IsZero())
	})

	t.Run("single level", func(t *testing.T) {
		levels := []*GridLevel{{Price: decimal.NewFromInt(100)}}
		spacing := calc.GetGridSpacing(levels)
		assert.True(t, spacing.IsZero())
	})
}
