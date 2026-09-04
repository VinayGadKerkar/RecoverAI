import { useState, useEffect, useRef, useCallback } from 'react';
import { toast } from 'sonner';

export interface WSMessage {
  type: 'audit_event' | 'case_status_changed' | 'metric_update' | 
        'outage_detected' | 'pipeline_heartbeat';
  timestamp: string;
  case_id?: string;
  payment_id?: string;
  data: Record<string, unknown>;
}

export interface MetricUpdate {
  total_cases?: number;
  revenue_at_risk?: number;
  revenue_recovered?: number;
  recovery_rate?: number;
  pending_human_approval?: number;
  customer_self_recovered?: number;
  not_worth_recovering?: number;
}

export function useWebSocket(caseID?: string) {
  const [events, setEvents] = useState<WSMessage[]>([]);
  const [connected, setConnected] = useState(false);
  const [metrics, setMetrics] = useState<MetricUpdate | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<NodeJS.Timeout>();
  const reconnectAttempts = useRef(0);

  const connect = useCallback(() => {
    // Get WebSocket URL from env or default to localhost
    const wsUrl = process.env.NEXT_PUBLIC_WS_URL || 'ws://localhost:8080';
    const ws = new WebSocket(`${wsUrl}/ws`);
    
    ws.onopen = () => {
      console.log('[WebSocket] Connected');
      setConnected(true);
      reconnectAttempts.current = 0;
      clearTimeout(reconnectTimer.current);
    };
    
    ws.onclose = () => {
      console.log('[WebSocket] Disconnected');
      setConnected(false);
      
      // Auto-reconnect with exponential backoff (max 30 seconds)
      const delay = Math.min(2000 * Math.pow(1.5, reconnectAttempts.current), 30000);
      reconnectAttempts.current++;
      
      reconnectTimer.current = setTimeout(() => {
        console.log(`[WebSocket] Reconnecting... (attempt ${reconnectAttempts.current})`);
        connect();
      }, delay);
    };
    
    ws.onerror = (error) => {
      console.error('[WebSocket] Error:', error);
      ws.close();
    };
    
    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data);
        
        // Filter by caseID if specified
        if (caseID && msg.case_id && msg.case_id !== caseID) {
          return;
        }
        
        switch (msg.type) {
          case 'audit_event':
            // Keep last 100 events to prevent memory issues
            setEvents(prev => [...prev.slice(-99), msg]);
            
            // Show toast notifications for status-changing audit events
            if (msg.data.action) {
              const { action, new_status, amount_paise, upi_error_code } = msg.data as {
                action?: string;
                new_status?: string;
                amount_paise?: number;
                upi_error_code?: string;
              };
              const amount = amount_paise ? `₹${(amount_paise / 100).toLocaleString('en-IN')}` : '';
              
              // Map actions to toast notifications
              if (action === 'payment_captured' || new_status === 'recovered') {
                toast.success(`${amount} recovered`, {
                  description: 'Payment recovery completed',
                  duration: 5000,
                });
              } else if (action === 'self_recovered' || new_status === 'customer_self_recovered') {
                toast.info('Customer self-recovered', {
                  description: 'No system action needed',
                  duration: 4000,
                });
              } else if (action === 'stopped' || new_status === 'not_worth_recovering') {
                toast('Recovery stopped', {
                  description: 'Negative ROI — not cost effective',
                  duration: 4000,
                });
              } else if (action === 'human_approval_required' || new_status === 'pending_human_approval') {
                toast.warning(`${amount} needs approval`, {
                  description: 'High-value case requires manual review',
                  duration: 6000,
                });
              } else if (action === 'bank_outage_detected') {
                toast.warning(`Bank outage detected: ${upi_error_code || 'Unknown'}`, {
                  description: 'Cases batched until bank recovers',
                  duration: 8000,
                });
              }
            }
            break;
            
          case 'case_status_changed':
            // Legacy support for explicit status change events
            setEvents(prev => [...prev.slice(-99), msg]);
            
            const { new_status, amount_paise } = msg.data as {
              new_status?: string;
              amount_paise?: number;
            };
            const amount = amount_paise ? `₹${(amount_paise / 100).toLocaleString('en-IN')}` : '';
            
            switch (new_status) {
              case 'recovered':
                toast.success(`${amount} recovered`, {
                  description: 'Payment recovery completed',
                  duration: 5000,
                });
                break;
              case 'customer_self_recovered':
                toast.info('Customer self-recovered', {
                  description: 'No system action needed',
                  duration: 4000,
                });
                break;
              case 'not_worth_recovering':
                toast('Recovery stopped', {
                  description: 'Negative ROI — not cost effective',
                  duration: 4000,
                });
                break;
              case 'pending_human_approval':
                toast.warning(`${amount} needs approval`, {
                  description: 'High-value case requires manual review',
                  duration: 6000,
                });
                break;
            }
            break;
            
          case 'outage_detected':
            setEvents(prev => [...prev.slice(-99), msg]);
            
            const { upi_error_code, failure_count } = msg.data as {
              upi_error_code?: string;
              failure_count?: number;
            };
            toast.warning(`Bank outage detected: ${upi_error_code}`, {
              description: `${failure_count} failures in 5 minutes — cases batched`,
              duration: 8000,
            });
            break;
            
          case 'metric_update':
            setMetrics(msg.data as MetricUpdate);
            break;
            
          case 'pipeline_heartbeat':
            // Heartbeat - no action needed, just keeps connection alive
            break;
            
          default:
            console.warn('[WebSocket] Unknown message type:', msg.type);
        }
      } catch (error) {
        console.error('[WebSocket] Failed to parse message:', error);
      }
    };
    
    wsRef.current = ws;
  }, [caseID]);

  useEffect(() => {
    connect();
    
    return () => {
      clearTimeout(reconnectTimer.current);
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [connect]);

  return { events, connected, metrics };
}
