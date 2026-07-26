package service

import (
	"testing"

	"chldemo/internal/upay"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name       string
		order      upay.Order
		wantAct    action
		wantReason string
	}{
		{"payment_status PAID activates", upay.Order{Status: "PROCESSING", PaymentStatus: "PAID"}, actActivate, ""},
		{"payment_status OVERPAID activates", upay.Order{Status: "PROCESSING", PaymentStatus: "OVERPAID"}, actActivate, ""},
		{"legacy COMPLETED without payment_status activates", upay.Order{Status: "COMPLETED"}, actActivate, ""},
		{"legacy PAID alias activates", upay.Order{Status: "PAID"}, actActivate, ""},
		{"CLOSED fully paid activates", upay.Order{Status: "CLOSED", PaymentStatus: "PAID"}, actActivate, ""},
		{"CLOSED unpaid is terminal", upay.Order{Status: "CLOSED", PaymentStatus: "UNPAID"}, actTerminal, "closed_unpaid"},
		{"CANCELED carries cancel_reason", upay.Order{Status: "CANCELED", CancelReason: "user_cancel"}, actTerminal, "user_cancel"},
		{"REJECTED maps to kyc_rejected", upay.Order{Status: "REJECTED", PaymentStatus: "UNPAID"}, actTerminal, "kyc_rejected"},
		{"INITED unpaid waits", upay.Order{Status: "INITED", PaymentStatus: "UNPAID"}, actWait, ""},
		{"PENDING_APPROVAL waits", upay.Order{Status: "PENDING_APPROVAL"}, actWait, ""},
		{"REJECTING waits for terminal state", upay.Order{Status: "REJECTING"}, actWait, ""},
		{"PROCESSING partial waits", upay.Order{Status: "PROCESSING", PaymentStatus: "PARTIAL"}, actWait, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act, reason := classify(&tt.order)
			if act != tt.wantAct || reason != tt.wantReason {
				t.Errorf("classify(%+v) = (%v, %q), want (%v, %q)",
					tt.order, act, reason, tt.wantAct, tt.wantReason)
			}
		})
	}
}
