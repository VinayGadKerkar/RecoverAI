package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestMockMode(t *testing.T) {
	// Create test servers
	realServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Should not call real AI in mock mode")
	}))
	defer realServer.Close()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" {
			t.Errorf("Expected /analyze path, got %s", r.URL.Path)
		}
		
		// Return mock response
		response := AnalyzeResponse{
			Action:             "RETRY_PAYMENT",
			PaymentID:          "pay_test",
			CaseID:             "case_test",
			ScheduledAtMinutes: 10,
			Parameters:         map[string]interface{}{"retry_reason": "mock test"},
			Mock:               true,
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	// Set environment to mock mode
	os.Setenv("USE_MOCK_AI", "true")
	os.Setenv("AI_SERVICE_URL", realServer.URL)
	os.Setenv("MOCK_AI_URL", mockServer.URL)
	defer func() {
		os.Unsetenv("USE_MOCK_AI")
		os.Unsetenv("AI_SERVICE_URL")
		os.Unsetenv("MOCK_AI_URL")
	}()

	// Create client
	client := NewAIClient()

	// Make request
	req := AnalyzeRequest{
		PaymentID:    "pay_test",
		CaseID:       "case_test",
		AmountPaise:  100000,
		UPIErrorCode: "U30",
	}

	ctx := context.Background()
	resp, err := client.Analyze(ctx, req)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Verify response came from mock
	if !resp.Mock {
		t.Error("Expected mock response, got real AI response")
	}

	// Verify mode
	if client.GetMode() != "mock" {
		t.Errorf("Expected mode 'mock', got '%s'", client.GetMode())
	}

	// Verify URL
	if client.GetURL() != mockServer.URL+"/analyze" {
		t.Errorf("Expected mock URL, got '%s'", client.GetURL())
	}
}

func TestRealMode(t *testing.T) {
	// Create test servers
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Should not call mock AI in real mode")
	}))
	defer mockServer.Close()

	realServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" {
			t.Errorf("Expected /analyze path, got %s", r.URL.Path)
		}
		
		// Return real AI response (no _mock field)
		response := AnalyzeResponse{
			Action:             "RETRY_PAYMENT",
			PaymentID:          "pay_test",
			CaseID:             "case_test",
			ScheduledAtMinutes: 10,
			Parameters:         map[string]interface{}{"retry_reason": "real AI decision"},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer realServer.Close()

	// Set environment to real mode
	os.Setenv("USE_MOCK_AI", "false")
	os.Setenv("AI_SERVICE_URL", realServer.URL)
	os.Setenv("MOCK_AI_URL", mockServer.URL)
	defer func() {
		os.Unsetenv("USE_MOCK_AI")
		os.Unsetenv("AI_SERVICE_URL")
		os.Unsetenv("MOCK_AI_URL")
	}()

	// Create client
	client := NewAIClient()

	// Make request
	req := AnalyzeRequest{
		PaymentID:    "pay_test",
		CaseID:       "case_test",
		AmountPaise:  100000,
		UPIErrorCode: "U30",
	}

	ctx := context.Background()
	resp, err := client.Analyze(ctx, req)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Verify response came from real AI (no mock field)
	if resp.Mock {
		t.Error("Expected real AI response, got mock response")
	}

	// Verify mode
	if client.GetMode() != "real" {
		t.Errorf("Expected mode 'real', got '%s'", client.GetMode())
	}

	// Verify URL
	if client.GetURL() != realServer.URL+"/analyze" {
		t.Errorf("Expected real URL, got '%s'", client.GetURL())
	}
}

func TestMockUnreachable(t *testing.T) {
	// Set environment to mock mode with invalid URL
	os.Setenv("USE_MOCK_AI", "true")
	os.Setenv("MOCK_AI_URL", "http://localhost:99999") // Invalid port
	defer func() {
		os.Unsetenv("USE_MOCK_AI")
		os.Unsetenv("MOCK_AI_URL")
	}()

	// Create client
	client := NewAIClient()

	// Make request (should not panic)
	req := AnalyzeRequest{
		PaymentID:    "pay_test",
		CaseID:       "case_test",
		AmountPaise:  100000,
		UPIErrorCode: "U30",
	}

	ctx := context.Background()
	resp, err := client.Analyze(ctx, req)
	
	// Should not return error (graceful fallback)
	if err != nil {
		t.Fatalf("Expected graceful fallback, got error: %v", err)
	}

	// Should return STOP command
	if resp.Action != "STOP" {
		t.Errorf("Expected STOP action for unreachable mock, got '%s'", resp.Action)
	}

	// Should be marked as mock
	if !resp.Mock {
		t.Error("Expected mock flag for fallback response")
	}

	// Should have fallback reason in parameters
	if reason, ok := resp.Parameters["reason"].(string); !ok || reason == "" {
		t.Error("Expected fallback reason in parameters")
	}
}

func TestTestAILimit(t *testing.T) {
	callCount := 0
	realServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		response := AnalyzeResponse{
			Action:             "RETRY_PAYMENT",
			PaymentID:          "pay_test",
			CaseID:             "case_test",
			ScheduledAtMinutes: 10,
			Parameters:         map[string]interface{}{},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer realServer.Close()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := AnalyzeResponse{
			Action:             "RETRY_PAYMENT",
			PaymentID:          "pay_test",
			CaseID:             "case_test",
			ScheduledAtMinutes: 10,
			Parameters:         map[string]interface{}{},
			Mock:               true,
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	// Set environment with TEST_AI_LIMIT=3
	os.Setenv("USE_MOCK_AI", "false") // Start in real mode
	os.Setenv("TEST_AI_LIMIT", "3")
	os.Setenv("AI_SERVICE_URL", realServer.URL)
	os.Setenv("MOCK_AI_URL", mockServer.URL)
	defer func() {
		os.Unsetenv("USE_MOCK_AI")
		os.Unsetenv("TEST_AI_LIMIT")
		os.Unsetenv("AI_SERVICE_URL")
		os.Unsetenv("MOCK_AI_URL")
	}()

	// Create client
	client := NewAIClient()

	req := AnalyzeRequest{
		PaymentID:    "pay_test",
		CaseID:       "case_test",
		AmountPaise:  100000,
		UPIErrorCode: "U30",
	}

	ctx := context.Background()

	// Make 3 real AI calls
	for i := 0; i < 3; i++ {
		resp, err := client.Analyze(ctx, req)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i+1, err)
		}
		if resp.Mock {
			t.Errorf("Call %d should be real AI, got mock", i+1)
		}
	}

	// Verify 3 real calls were made
	if callCount != 3 {
		t.Errorf("Expected 3 real API calls, got %d", callCount)
	}

	// Next calls should automatically use mock
	for i := 0; i < 3; i++ {
		resp, err := client.Analyze(ctx, req)
		if err != nil {
			t.Fatalf("Call %d (after limit) failed: %v", i+4, err)
		}
		if !resp.Mock {
			t.Errorf("Call %d (after limit) should be mock, got real", i+4)
		}
	}

	// Verify no additional real calls were made
	if callCount != 3 {
		t.Errorf("Expected still 3 real API calls after limit, got %d", callCount)
	}

	// Verify mode switched to mock
	if client.GetMode() != "mock" {
		t.Errorf("Expected mode 'mock' after limit, got '%s'", client.GetMode())
	}
}

func TestIsMockAvailable(t *testing.T) {
	// Create healthy mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"mock":   true,
				"status": "ok",
			})
		}
	}))
	defer mockServer.Close()

	// Set environment
	os.Setenv("MOCK_AI_URL", mockServer.URL)
	defer os.Unsetenv("MOCK_AI_URL")

	client := NewAIClient()

	// Test healthy mock
	ctx := context.Background()
	if !client.IsMockAvailable(ctx) {
		t.Error("Expected mock to be available")
	}

	// Test unreachable mock
	client.mockURL = "http://localhost:99999"
	if client.IsMockAvailable(ctx) {
		t.Error("Expected mock to be unavailable with bad URL")
	}
}

func TestConcurrentCalls(t *testing.T) {
	// Test that atomic counter works correctly under concurrent load
	realServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Simulate some latency
		response := AnalyzeResponse{
			Action:             "RETRY_PAYMENT",
			PaymentID:          "pay_test",
			CaseID:             "case_test",
			ScheduledAtMinutes: 10,
			Parameters:         map[string]interface{}{},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer realServer.Close()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := AnalyzeResponse{
			Action:             "RETRY_PAYMENT",
			PaymentID:          "pay_test",
			CaseID:             "case_test",
			ScheduledAtMinutes: 10,
			Parameters:         map[string]interface{}{},
			Mock:               true,
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	os.Setenv("USE_MOCK_AI", "false")
	os.Setenv("TEST_AI_LIMIT", "10")
	os.Setenv("AI_SERVICE_URL", realServer.URL)
	os.Setenv("MOCK_AI_URL", mockServer.URL)
	defer func() {
		os.Unsetenv("USE_MOCK_AI")
		os.Unsetenv("TEST_AI_LIMIT")
		os.Unsetenv("AI_SERVICE_URL")
		os.Unsetenv("MOCK_AI_URL")
	}()

	client := NewAIClient()

	// Make 20 concurrent calls
	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func() {
			req := AnalyzeRequest{
				PaymentID:    "pay_test",
				CaseID:       "case_test",
				AmountPaise:  100000,
				UPIErrorCode: "U30",
			}
			ctx := context.Background()
			_, err := client.Analyze(ctx, req)
			if err != nil {
				t.Errorf("Concurrent call failed: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all calls to complete
	for i := 0; i < 20; i++ {
		<-done
	}

	// Verify counter is correct (should be 10, then switched to mock)
	finalCount := atomic.LoadInt32(&client.realCallCount)
	if finalCount != 10 {
		t.Errorf("Expected real call count to be 10, got %d", finalCount)
	}
}

func TestDefaultURLs(t *testing.T) {
	// Clear environment
	os.Unsetenv("AI_SERVICE_URL")
	os.Unsetenv("MOCK_AI_URL")

	client := NewAIClient()

	// Verify defaults
	if client.realURL != "http://localhost:8000" {
		t.Errorf("Expected default real URL 'http://localhost:8000', got '%s'", client.realURL)
	}

	if client.mockURL != "http://localhost:8001" {
		t.Errorf("Expected default mock URL 'http://localhost:8001', got '%s'", client.mockURL)
	}
}
