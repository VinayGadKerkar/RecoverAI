'use client';

import { useEffect, useRef, useMemo } from 'react';
import { WSMessage } from '@/hooks/useWebSocket';
import { AuditLogEntry } from '@/lib/types';

interface AuditTimelineProps {
  staticEvents: AuditLogEntry[];
  liveEvents: WSMessage[];
  isLive: boolean;
}

interface ActorConfig {
  icon: string;
  color: string;
  label: string;
}

const actorConfigs: Record<string, ActorConfig> = {
  system: { icon: '⚙️', color: '#6B7280', label: 'System' },
  risk_engine: { icon: '📊', color: '#3B82F6', label: 'Risk Engine' },
  validator: { icon: '🛡️', color: '#10B981', label: 'Validator' },
  validator_fail: { icon: '🛡️', color: '#EF4444', label: 'Validator' },
  ai_agent: { icon: '🤖', color: '#8B5CF6', label: 'AI Agent' },
  policy_engine: { icon: '⚖️', color: '#F59E0B', label: 'Policy Engine' },
  execution_worker: { icon: '⚡', color: '#F97316', label: 'Execution' },
  human: { icon: '👤', color: '#6366F1', label: 'Human' },
  customer_self: { icon: '👤', color: '#10B981', label: 'Customer' },
};

interface TimelineEvent {
  timestamp: string;
  actor: string;
  action: string;
  message: string;
  metadata?: Record<string, unknown>;
  isRecovery?: boolean;
  amount?: number;
}

export default function AuditTimeline({ staticEvents, liveEvents, isLive }: AuditTimelineProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  // Convert static events to timeline format
  const staticTimelineEvents: TimelineEvent[] = useMemo(() => {
    return staticEvents.map(event => ({
      timestamp: event.created_at,
      actor: event.actor,
      action: event.action,
      message: formatMessage(event.action, event.details || {}),
      metadata: event.details || {},
      isRecovery: event.action === 'payment_captured',
      amount: event.details?.amount_paise as number | undefined,
    }));
  }, [staticEvents]);

  // Convert live events to timeline format
  const liveTimelineEvents: TimelineEvent[] = useMemo(() => {
    return liveEvents
      .filter(event => event.type === 'audit_event')
      .map(event => ({
        timestamp: event.timestamp,
        actor: (event.data.actor as string) || 'system',
        action: (event.data.action as string) || 'unknown',
        message: formatMessage((event.data.action as string) || 'unknown', event.data.metadata as Record<string, unknown> || {}),
        metadata: event.data.metadata as Record<string, unknown>,
        isRecovery: event.data.action === 'payment_captured',
        amount: (event.data.metadata as any)?.amount_paise,
      }));
  }, [liveEvents]);

  // Merge and deduplicate events
  const allEvents = useMemo(() => {
    const combined = [...staticTimelineEvents, ...liveTimelineEvents];
    
    // Sort by timestamp
    combined.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
    
    // Deduplicate: same actor + action + within 1 second = skip
    const deduplicated: TimelineEvent[] = [];
    for (let i = 0; i < combined.length; i++) {
      const current = combined[i];
      const prev = deduplicated[deduplicated.length - 1];
      
      if (!prev) {
        deduplicated.push(current);
        continue;
      }
      
      const timeDiff = Math.abs(
        new Date(current.timestamp).getTime() - new Date(prev.timestamp).getTime()
      );
      
      const isDuplicate = 
        prev.actor === current.actor &&
        prev.action === current.action &&
        timeDiff < 1000;
      
      if (!isDuplicate) {
        deduplicated.push(current);
      }
    }
    
    return deduplicated;
  }, [staticTimelineEvents, liveTimelineEvents]);

  // Auto-scroll to bottom when new events arrive
  useEffect(() => {
    if (liveEvents.length > 0) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [liveEvents.length]);

  return (
    <div className="space-y-4">
      {/* Live status badge */}
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold">Audit Timeline</h3>
        <div className="flex items-center gap-2">
          {isLive ? (
            <>
              <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
              <span className="text-sm text-green-400 font-medium">LIVE</span>
            </>
          ) : (
            <>
              <div className="w-2 h-2 bg-gray-500 rounded-full" />
              <span className="text-sm text-gray-500">RECONNECTING...</span>
            </>
          )}
        </div>
      </div>

      {/* Timeline */}
      <div className="space-y-1 max-h-[600px] overflow-y-auto pr-2">
        {allEvents.length === 0 ? (
          <div className="text-center py-8 text-gray-500">
            No events yet. Waiting for activity...
          </div>
        ) : (
          allEvents.map((event, index) => (
            <TimelineEntry key={`${event.timestamp}-${index}`} event={event} />
          ))
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}

function TimelineEntry({ event }: { event: TimelineEvent }) {
  const actorKey = event.action.includes('failed') || event.action.includes('blocked')
    ? 'validator_fail'
    : event.actor;
  
  const actor = actorConfigs[actorKey] || actorConfigs.system;
  
  const time = new Date(event.timestamp).toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });

  const isRecovery = event.isRecovery;
  const borderColor = isRecovery ? '#10B981' : actor.color;
  const bgColor = isRecovery ? 'rgba(16, 185, 129, 0.05)' : 'transparent';

  return (
    <div
      className="flex items-start gap-3 py-2 border-l-2 pl-3 rounded-r animate-slideIn"
      style={{ borderColor, background: bgColor }}
    >
      <span className="text-xs text-gray-500 w-20 shrink-0 font-mono">{time}</span>
      <span className="text-lg">{actor.icon}</span>
      <div className="flex-1">
        <span className="text-sm font-medium" style={{ color: actor.color }}>
          {actor.label}
        </span>
        <p className={`text-sm ${isRecovery ? 'text-green-400 font-bold text-base' : 'text-gray-300'}`}>
          {event.message}
        </p>
        {event.metadata && Object.keys(event.metadata).length > 0 && (
          <p className="text-xs text-gray-500 mt-1">
            {formatMetadata(event.metadata)}
          </p>
        )}
      </div>
    </div>
  );
}

function formatMessage(action: string, metadata: Record<string, unknown>): string {
  const amount = metadata?.amount_paise as number;
  const errorCode = metadata?.error_code as string;
  const confidence = metadata?.confidence as number;
  const reason = metadata?.reason as string;
  
  switch (action) {
    case 'payment_failed':
      return `Payment failed${errorCode ? ` with ${errorCode}` : ''}`;
    
    case 'risk_scored':
      return `Risk assessment completed${metadata?.risk_score ? ` (score: ${(metadata.risk_score as number * 100).toFixed(0)}%)` : ''}`;
    
    case 'validator_pass':
      return `Validation passed${reason ? `: ${reason}` : ''}`;
    
    case 'validator_blocked':
    case 'validator_failed':
      return `Validation blocked${reason ? `: ${reason}` : ''}`;
    
    case 'ai_analysis_started':
      return 'AI analysis initiated';
    
    case 'ai_recommendation':
      return `AI recommends ${metadata?.recommended_action || 'action'}${confidence ? ` (${(confidence * 100).toFixed(0)}% confidence)` : ''}`;
    
    case 'policy_approved':
      return 'Policy engine approved action';
    
    case 'policy_blocked':
      return `Policy blocked${reason ? `: ${reason}` : ''}`;
    
    case 'action_executed':
      return `${metadata?.action_type || 'Action'} executed`;
    
    case 'retry_attempted':
      return 'Retry payment attempted';
    
    case 'payment_captured':
      if (amount) {
        const rupees = (amount / 100).toFixed(2);
        return `✅ ₹${rupees} RECOVERED`;
      }
      return '✅ Payment recovered';
    
    case 'case_finalized':
      return `Case finalized${metadata?.final_status ? ` as ${metadata.final_status}` : ''}`;
    
    default:
      return action.replace(/_/g, ' ');
  }
}

function formatMetadata(metadata: Record<string, unknown>): string {
  const parts: string[] = [];
  
  if (metadata.payment_id) parts.push(`Payment: ${metadata.payment_id}`);
  if (metadata.case_id) parts.push(`Case: ${metadata.case_id}`);
  if (metadata.error_code) parts.push(`Error: ${metadata.error_code}`);
  if (metadata.reasoning) parts.push(`Reason: ${metadata.reasoning}`);
  
  return parts.join(' • ');
}
