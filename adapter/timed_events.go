package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

type timedEventSession struct {
	streamID   string
	cancel     context.CancelFunc
	acks       chan protocol.TimedEventAckParams
	done       chan struct{}
	ready      chan struct{}
	readyOnce  sync.Once
	emitMu     sync.Mutex
	pendingAck *protocol.TimedEventAckParams
}

type timedEventStreams struct {
	ctx     context.Context
	output  io.Writer
	writeMu sync.Mutex
	mu      sync.Mutex
	streams map[string]*timedEventSession
}

func newTimedEventStreams(ctx context.Context, output io.Writer) *timedEventStreams {
	return &timedEventStreams{ctx: ctx, output: output, streams: make(map[string]*timedEventSession)}
}

func (s *timedEventStreams) writeResponse(response protocol.Response) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return protocol.WriteResponse(s.output, response)
}

func (s *timedEventStreams) dispatch(ctx context.Context, a Adapter, d protocol.Descriptor, req protocol.Request) (protocol.Response, bool, bool) {
	if req.Method != protocol.MethodEventsStart && req.Method != protocol.MethodEventsAck && req.Method != protocol.MethodEventsStop {
		return protocol.Response{}, false, false
	}
	if !has(d, protocol.CapabilityEvents) {
		return failure(req, "unsupported_method", "method is not supported"), false, true
	}
	switch req.Method {
	case protocol.MethodEventsStart:
		provider, ok := a.(TimedEventProvider)
		if !ok {
			return failure(req, "unsupported_method", "method is not supported"), false, true
		}
		var params protocol.TimedEventStartParams
		if err := decodeParams(req.Params, &params); err != nil || strings.TrimSpace(params.StreamID) == "" || len(params.StreamID) > 128 || len(params.ResumeState) > protocol.MaxTimedEventResumeBytes || len(params.ResumeState) > 0 && !json.Valid(params.ResumeState) {
			return failure(req, "invalid_params", "timed event stream parameters are invalid"), false, true
		}
		streamCtx, cancel := context.WithCancel(ctx)
		session := &timedEventSession{streamID: params.StreamID, cancel: cancel, acks: make(chan protocol.TimedEventAckParams, 1), done: make(chan struct{}), ready: make(chan struct{})}
		s.mu.Lock()
		if _, exists := s.streams[params.StreamID]; exists {
			s.mu.Unlock()
			cancel()
			return failure(req, "stream_exists", "timed event stream already exists"), false, true
		}
		if len(s.streams) >= 16 {
			s.mu.Unlock()
			cancel()
			return failure(req, "stream_limit", "timed event stream limit reached"), false, true
		}
		s.streams[params.StreamID] = session
		s.mu.Unlock()
		go func() {
			defer close(session.done)
			defer func() {
				s.mu.Lock()
				if s.streams[session.streamID] == session {
					delete(s.streams, session.streamID)
				}
				s.mu.Unlock()
			}()
			select {
			case <-session.ready:
			case <-streamCtx.Done():
				return
			}
			streamErr := provider.StreamTimedEvents(streamCtx, params, func(batch protocol.TimedEventBatch) error { return s.emit(streamCtx, session, batch) })
			reason := "completed"
			if streamErr != nil {
				reason = "failed"
			}
			_ = s.writeNotification(protocol.MethodEventsEnded, map[string]string{"stream_id": session.streamID, "reason": reason})
		}()
		return success(req, protocol.TimedEventStartResult{Accepted: true}), false, true
	case protocol.MethodEventsAck:
		var params protocol.TimedEventAckParams
		if err := decodeParams(req.Params, &params); err != nil || params.StreamID == "" || params.BatchID == "" {
			return failure(req, "invalid_params", "timed event acknowledgement is invalid"), false, true
		}
		s.mu.Lock()
		session := s.streams[params.StreamID]
		if session == nil {
			s.mu.Unlock()
			return failure(req, "stream_not_found", "timed event stream is not active"), false, true
		}
		if session.pendingAck != nil {
			s.mu.Unlock()
			return failure(req, "unexpected_ack", "timed event acknowledgement is not expected"), false, true
		}
		copy := params
		session.pendingAck = &copy
		s.mu.Unlock()
		return success(req, struct{}{}), false, true
	case protocol.MethodEventsStop:
		var params protocol.TimedEventStopParams
		if err := decodeParams(req.Params, &params); err != nil || params.StreamID == "" {
			return failure(req, "invalid_params", "timed event stop request is invalid"), false, true
		}
		s.mu.Lock()
		session := s.streams[params.StreamID]
		s.mu.Unlock()
		if session != nil {
			session.cancel()
			select {
			case <-session.done:
			case <-time.After(2 * time.Second):
			}
		}
		return success(req, struct{}{}), false, true
	default:
		return protocol.Response{}, false, false
	}
}

func (s *timedEventStreams) writeNotification(method string, params any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return errors.New("timed event notification could not be encoded")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return protocol.WriteNotification(s.output, protocol.Notification{ProtocolVersion: protocol.Version, Method: method, Params: encoded})
}

func (s *timedEventStreams) responseWritten(req protocol.Request) {
	switch req.Method {
	case protocol.MethodEventsStart:
		var params protocol.TimedEventStartParams
		if decodeParams(req.Params, &params) != nil || params.StreamID == "" {
			return
		}
		s.mu.Lock()
		session := s.streams[params.StreamID]
		s.mu.Unlock()
		if session != nil {
			session.readyOnce.Do(func() { close(session.ready) })
		}
	case protocol.MethodEventsAck:
		var params protocol.TimedEventAckParams
		if decodeParams(req.Params, &params) != nil || params.StreamID == "" {
			return
		}
		s.mu.Lock()
		session := s.streams[params.StreamID]
		if session == nil || session.pendingAck == nil {
			s.mu.Unlock()
			return
		}
		ack := *session.pendingAck
		session.pendingAck = nil
		s.mu.Unlock()
		select {
		case session.acks <- ack:
		case <-session.done:
		default:
		}
	}
}

func (s *timedEventStreams) emit(ctx context.Context, session *timedEventSession, batch protocol.TimedEventBatch) error {
	session.emitMu.Lock()
	defer session.emitMu.Unlock()
	if batch.StreamID != session.streamID || batch.Validate() != nil {
		return errors.New("timed event batch is invalid")
	}
	params, err := json.Marshal(batch)
	if err != nil {
		return errors.New("timed event batch is invalid")
	}
	s.writeMu.Lock()
	err = protocol.WriteNotification(s.output, protocol.Notification{ProtocolVersion: protocol.Version, Method: protocol.MethodEventsBatch, Params: params})
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("timed event notification could not be written")
	}
	select {
	case ack := <-session.acks:
		if ack.StreamID != batch.StreamID || ack.BatchID != batch.BatchID {
			return errors.New("timed event acknowledgement does not match the outstanding batch")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-session.done:
		return context.Canceled
	}
}

func (s *timedEventStreams) close() {
	s.mu.Lock()
	streams := make([]*timedEventSession, 0, len(s.streams))
	for _, session := range s.streams {
		streams = append(streams, session)
		session.cancel()
	}
	s.mu.Unlock()
	for _, session := range streams {
		select {
		case <-session.done:
		case <-time.After(2 * time.Second):
		}
	}
}

func success(req protocol.Request, result any) protocol.Response {
	response, err := protocol.Success(req.ID, result)
	if err != nil {
		return failure(req, "internal_error", "adapter operation failed")
	}
	return response
}
