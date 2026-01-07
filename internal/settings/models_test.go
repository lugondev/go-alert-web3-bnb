package settings

import "testing"

func TestShouldNotifyTx(t *testing.T) {
	tests := []struct {
		name       string
		config     StreamNotifyConfig
		txType     string
		valueUSD   float64
		want       bool
		wantReason string
	}{
		{
			name: "disabled config blocks all",
			config: StreamNotifyConfig{
				Enabled: false,
			},
			txType:     "buy",
			valueUSD:   100,
			want:       false,
			wantReason: "config disabled",
		},
		{
			name: "below min value blocks tx",
			config: StreamNotifyConfig{
				Enabled:       true,
				TxMinValueUSD: 1000,
			},
			txType:     "buy",
			valueUSD:   500,
			want:       false,
			wantReason: "below min value",
		},
		{
			name: "above min value passes",
			config: StreamNotifyConfig{
				Enabled:       true,
				TxMinValueUSD: 1000,
			},
			txType:     "buy",
			valueUSD:   1500,
			want:       true,
			wantReason: "above min value",
		},
		{
			name: "filter buy blocks sell",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterBuy,
			},
			txType:     "sell",
			valueUSD:   100,
			want:       false,
			wantReason: "filter buy blocks sell",
		},
		{
			name: "filter buy allows buy",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterBuy,
			},
			txType:     "buy",
			valueUSD:   100,
			want:       true,
			wantReason: "filter buy allows buy",
		},
		{
			name: "filter sell blocks buy",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterSell,
			},
			txType:     "buy",
			valueUSD:   100,
			want:       false,
			wantReason: "filter sell blocks buy",
		},
		{
			name: "filter sell allows sell",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterSell,
			},
			txType:     "sell",
			valueUSD:   100,
			want:       true,
			wantReason: "filter sell allows sell",
		},
		{
			name: "empty txType with buy filter passes",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterBuy,
			},
			txType:     "",
			valueUSD:   100,
			want:       true,
			wantReason: "empty txType should pass when filter is set",
		},
		{
			name: "empty txType with sell filter passes",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterSell,
			},
			txType:     "",
			valueUSD:   100,
			want:       true,
			wantReason: "empty txType should pass when filter is set",
		},
		{
			name: "empty txType with no filter passes",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterBoth,
			},
			txType:     "",
			valueUSD:   100,
			want:       true,
			wantReason: "empty txType with no filter should pass",
		},
		{
			name: "unknown txType with buy filter passes",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterBuy,
			},
			txType:     "swap",
			valueUSD:   100,
			want:       false,
			wantReason: "unknown txType 'swap' should be filtered when filter is buy",
		},
		{
			name: "both filter allows buy",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterBoth,
			},
			txType:     "buy",
			valueUSD:   100,
			want:       true,
			wantReason: "both filter allows buy",
		},
		{
			name: "both filter allows sell",
			config: StreamNotifyConfig{
				Enabled:      true,
				TxFilterType: TxFilterBoth,
			},
			txType:     "sell",
			valueUSD:   100,
			want:       true,
			wantReason: "both filter allows sell",
		},
		{
			name: "combined filters: buy + min value blocks low value buy",
			config: StreamNotifyConfig{
				Enabled:       true,
				TxFilterType:  TxFilterBuy,
				TxMinValueUSD: 1000,
			},
			txType:     "buy",
			valueUSD:   500,
			want:       false,
			wantReason: "low value buy should be blocked",
		},
		{
			name: "combined filters: buy + min value allows high value buy",
			config: StreamNotifyConfig{
				Enabled:       true,
				TxFilterType:  TxFilterBuy,
				TxMinValueUSD: 1000,
			},
			txType:     "buy",
			valueUSD:   1500,
			want:       true,
			wantReason: "high value buy should pass",
		},
		{
			name: "combined filters: buy + min value blocks sell even if high value",
			config: StreamNotifyConfig{
				Enabled:       true,
				TxFilterType:  TxFilterBuy,
				TxMinValueUSD: 1000,
			},
			txType:     "sell",
			valueUSD:   1500,
			want:       false,
			wantReason: "sell should be blocked even with high value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ShouldNotifyTx(tt.txType, tt.valueUSD)
			if got != tt.want {
				t.Errorf("ShouldNotifyTx() = %v, want %v (reason: %s)", got, tt.want, tt.wantReason)
			}
		})
	}
}
