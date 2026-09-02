import { useState, useEffect, useRef, useCallback } from 'react';

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
          case 'case_status_changed':
          case 'outage_detected':
            // Keep last 100 events to prevent memory issues
            setEvents(prev => [...prev.slice(-99), msg]);
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
