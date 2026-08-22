package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"recoverai/internal/config"
)

const razorpayBaseURL = "https://api.razorpay.com/v1"

// RazorpayService wraps the Razorpay REST API for payment operations.
// All operations are read-only or create new transactions — never modify existing ones.
type RazorpayService struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewRazorpayService(cfg *config.Config) *RazorpayService {
	return &RazorpayService{
		cfg: cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// CreatePaymentLink creates a new payment link for a failed payment.
// https://razorpay.com/docs/api/payment-links/
func (s *RazorpayService) CreatePaymentLink(ctx context.Context, paymentID string, amount int64, currency, customerEmail, customerPhone string) (string, error) {
	body := map[string]any{
		"amount":      amount,
		"currency":    currency,
		"description": fmt.Sprintf("Payment recovery for order related to payment %s", paymentID),
		"customer": map[string]string{
			"email": customerEmail,
			"contact": customerPhone,
		},
		"notify": map[string]bool{
			"sms":   true,
			"email": true,
		},
		"reminder_enable": true,
		"expire_by":       time.Now().Add(24 * time.Hour).Unix(),
	}

	resp, err := s.doRequest(ctx, "POST", "/payment_links", body)
	if err != nil {
		return "", fmt.Errorf("create payment link: %w", err)
	}

	shortURL, ok := resp["short_url"].(string)
	if !ok {
		return "", fmt.Errorf("payment link response missing short_url")
	}
	return shortURL, nil
}

// GetPayment fetches a payment by ID from Razorpay.
func (s *RazorpayService) GetPayment(ctx context.Context, paymentID string) (map[string]any, error) {
	return s.doRequest(ctx, "GET", fmt.Sprintf("/payments/%s", paymentID), nil)
}

// doRequest executes an authenticated request against the Razorpay API.
func (s *RazorpayService) doRequest(ctx context.Context, method, path string, body map[string]any) (map[string]any, error) {
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, razorpayBaseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.cfg.RazorpayKeyID, s.cfg.RazorpayKeySecret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("razorpay request: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode razorpay response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("razorpay API error %d: %v", resp.StatusCode, result)
	}
	return result, nil
}
