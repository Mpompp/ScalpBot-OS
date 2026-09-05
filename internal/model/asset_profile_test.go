package model_test

import (
	"testing"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestDetectAssetClass(t *testing.T) {
	tests := []struct {
		symbol        string
		expectedClass model.AssetClass
		expectedGold  bool
		expectedForex bool
	}{
		{"XAUUSD", model.AssetClassGold, true, false},
		{"GOLD", model.AssetClassGold, true, false},
		{"XAUUSD.m", model.AssetClassGold, true, false},
		{"EURUSD", model.AssetClassForex, false, true},
		{"GBPUSD", model.AssetClassForex, false, true},
		{"USDJPY", model.AssetClassForex, false, true},
		{"BTCUSD", model.AssetClassCrypto, false, false},
		{"US30", model.AssetClassCrypto, false, false},
	}

	for _, tt := range tests {
		prof := model.DetectAssetClass(tt.symbol)
		if prof.Class != tt.expectedClass {
			t.Errorf("DetectAssetClass(%s).Class = %v, want %v", tt.symbol, prof.Class, tt.expectedClass)
		}
		if prof.IsGold != tt.expectedGold {
			t.Errorf("DetectAssetClass(%s).IsGold = %v, want %v", tt.symbol, prof.IsGold, tt.expectedGold)
		}
		if prof.IsForex != tt.expectedForex {
			t.Errorf("DetectAssetClass(%s).IsForex = %v, want %v", tt.symbol, prof.IsForex, tt.expectedForex)
		}
	}
}

func TestDetectAccountType(t *testing.T) {
	tests := []struct {
		symbol   string
		currency string
		balance  float64
		want     model.AccountType
	}{
		{"XAUUSDc", "USC", 1000, model.AccountTypeCent},
		{"GOLDc", "USD", 500, model.AccountTypeCent},
		{"XAUUSD", "USC", 2000, model.AccountTypeCent},
		{"XAUUSD.c", "EUR", 1000, model.AccountTypeCent},
		{"XAUUSDm", "USD", 10000, model.AccountTypeRegular},
		{"XAUUSD", "USD", 10000, model.AccountTypeRegular},
		{"XAUUSD.pro", "USD", 50000, model.AccountTypeRegular},
		{"XAUUSD", "EUR", 15000, model.AccountTypeRegular},
	}

	for _, tt := range tests {
		got := model.DetectAccountType(tt.symbol, tt.currency, tt.balance)
		if got != tt.want {
			t.Errorf("DetectAccountType(%s, %s, %.2f) = %v, want %v",
				tt.symbol, tt.currency, tt.balance, got, tt.want)
		}
	}
}

