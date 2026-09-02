'use client';

import { useEffect, useRef, useState, useCallback } from 'react';

export interface WSMessage {
  type: 'audit_event' | 'case_status_changed' | 'metric_update' | 'outage_detected' | 'pipeline_heartbeat';
  timestamp: string;
  case_id?: string;
  payment_id?: string;
  data: any;
}

export interface UseWebSocketOptions {
  onMessage?: (message: WSMessage) => void;
  onAuditEvent?: (message: WSMessage) => void;
  onMetricUpdate?: (message: WSMessage) => void;
  onCaseStatusChanged?: (message: WSMessage) => void;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
}

export function useWebSocket(url: string, options: UseWebSocketOptions = {}) {
  const {
    onMessage,
    onAuditEvent,
    onMetricUpdate,
    onCaseStatusChanged,
    reconnectInterval = 3000,
    maxReconnectAttempts = 10,
  } = options;

  const [isConnected, setIsConnected] = useState(false);
  const [lastMessage, setLastMessage] = useState<WSMessage | null>(null);
  const [error, setError] = useState<Error | null>(null);
  
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const shouldReconnectRef = useRef(true);

  const connect = useCallback(() => {
    try {
      // Close existing connection
      if (wsRef.current) {
        wsRef.current.close();
      }

      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        console.log('✅ WebSocket connected');
        setIsConnected(true);
        setError(null);
        reconnectAttemptsRef.current = 0;
      };

      ws.onmessage = (event) => {
        try {
          const message: WSMessage = JSON.parse(event.data);
          setLastMessage(message);

          // Call generic handler
          if (onMessage) {
            onMessage(message);
          }

          // Call specific handlers
          switch (message.type) {
            case 'audit_event':
              if (onAuditEvent) onAuditEvent(message);
              break;
            case 'metric_update':
              if (onMetricUpdate) onMetricUpdate(message);
              break;
            case 'case_status_changed':
              if (onCaseStatusChanged) onCaseStatusChanged(message);
              break;
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err);
        }
      };

      ws.onerror = (event) => {
        console.error('❌ WebSocket error:', event);
        setError(new Error('WebSocket connection error'));
      };

      ws.onclose = (event) => {
        console.log('🔌 WebSocket closed:', event.code, event.reason);
        setIsConnected(false);
        wsRef.current = null;

        // Attempt reconnection if enabled
        if (shouldReconnectRef.current && reconnectAttemptsRef.current < maxReconnectAttempts) {
          reconnectAttemptsRef.current++;
          console.log(`🔄 Reconnecting... (attempt ${reconnectAttemptsRef.current}/${maxReconnectAttempts})`);
          
          reconnectTimeoutRef.current = setTimeout(() => {
            connect();
          }, reconnectInterval);
        } else if (reconnectAttemptsRef.current >= maxReconnectAttempts) {
          setError(new Error('Max reconnection attempts reached'));
        }
      };
    } catch (err) {
      console.error('Failed to create WebSocket:', err);
      setError(err as Error);
    }
  }, [url, onMessage, onAuditEvent, onMetricUpdate, onCaseStatusChanged, reconnectInterval, maxReconnectAttempts]);

  const disconnect = useCallback(() => {
    shouldReconnectRef.current = false;
    
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }

    setIsConnected(false);
  }, []);

  const reconnect = useCallback(() => {
    reconnectAttemptsRef.current = 0;
    shouldReconnectRef.current = true;
    connect();
  }, [connect]);

  // Connect on mount
  useEffect(() => {
    shouldReconnectRef.current = true;
    connect();

    // Cleanup on unmount
    return () => {
      shouldReconnectRef.current = false;
      
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
      
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, [connect]);

  return {
    isConnected,
    lastMessage,
    error,
    reconnect,
    disconnect,
  };
}
