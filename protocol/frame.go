package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

var ErrUnsupportedProtocolVersion = errors.New("unsupported protocol version")

// ReadRequest reads exactly one newline-terminated request. The frame limit
// counts JSON payload bytes and excludes the LF delimiter.
func ReadRequest(r *bufio.Reader) (Request, error) {
	line, err := readFrame(r)
	if err != nil {
		return Request{}, err
	}
	f, err := ParseFrame(line)
	if err != nil {
		return Request{}, err
	}
	if f.Kind != FrameTypeRequest {
		return Request{}, fmt.Errorf("expected request frame")
	}
	return *f.Request, nil
}

// ReadResponse is provided for SDK tests and adapter-side tooling.
func ReadResponse(r *bufio.Reader) (Response, error) {
	line, err := readFrame(r)
	if err != nil {
		return Response{}, err
	}
	f, err := ParseFrame(line)
	if err != nil {
		return Response{}, err
	}
	if f.Kind != FrameTypeResponse {
		return Response{}, fmt.Errorf("expected response frame")
	}
	return *f.Response, nil
}

// ReadFrame reads and classifies one bounded v1 protocol frame.
func ReadFrame(r *bufio.Reader) (Frame, error) {
	line, err := readFrame(r)
	if err != nil {
		return Frame{}, err
	}
	return ParseFrame(line)
}

func WriteRequest(w io.Writer, req Request) error        { return writeFrame(w, req) }
func WriteResponse(w io.Writer, response Response) error { return writeFrame(w, response) }

func Success(id string, result any) (Response, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return Response{}, err
	}
	return Response{ProtocolVersion: Version, ID: id, Result: data}, nil
}

func WriteNotification(w io.Writer, notification Notification) error {
	if notification.ProtocolVersion != Version || strings.TrimSpace(notification.Method) == "" || len(notification.Params) > MaxFrameBytes || len(notification.Params) > 0 && !json.Valid(notification.Params) {
		return errors.New("invalid protocol notification")
	}
	return writeFrame(w, struct {
		ProtocolVersion int             `json:"protocol_version"`
		Type            FrameType       `json:"type"`
		Method          string          `json:"method"`
		Params          json.RawMessage `json:"params,omitempty"`
	}{notification.ProtocolVersion, FrameTypeNotification, notification.Method, notification.Params})
}

// ParseFrame validates the common v1 envelope shape. Notification frames are
// recognized for compatibility but are not delivered by Serve.
func ParseFrame(data []byte) (Frame, error) {
	if len(data) > MaxFrameBytes {
		return Frame{}, fmt.Errorf("protocol frame exceeds %d bytes", MaxFrameBytes)
	}
	if !utf8.Valid(data) {
		return Frame{}, errors.New("protocol frame is not valid UTF-8")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Frame{}, fmt.Errorf("malformed protocol frame JSON: %w", err)
	}
	if fields == nil {
		return Frame{}, errors.New("protocol frame must be a JSON object")
	}
	var version int
	raw, ok := fields["protocol_version"]
	if !ok || json.Unmarshal(raw, &version) != nil {
		return Frame{}, errors.New("protocol version is required")
	}
	if version != Version {
		return Frame{}, fmt.Errorf("%w %d", ErrUnsupportedProtocolVersion, version)
	}
	if rt, ok := fields["type"]; ok {
		var typ string
		if json.Unmarshal(rt, &typ) != nil || typ == "" {
			return Frame{}, errors.New("protocol frame type must be a nonempty string")
		}
		switch FrameType(typ) {
		case FrameTypeRequest:
			if _, ok := fields["result"]; ok {
				return Frame{}, errors.New("request frame cannot contain a result")
			}
			if _, ok := fields["error"]; ok {
				return Frame{}, errors.New("request frame cannot contain an error")
			}
			return parseRequest(data)
		case FrameTypeResponse:
			if _, ok := fields["method"]; ok {
				return Frame{}, errors.New("response frame cannot contain a method")
			}
			return parseResponse(data)
		case FrameTypeNotification:
			if _, ok := fields["id"]; ok {
				return Frame{}, errors.New("notification frame must not contain an id")
			}
			if _, ok := fields["result"]; ok {
				return Frame{}, errors.New("notification cannot contain a result")
			}
			if _, ok := fields["error"]; ok {
				return Frame{}, errors.New("notification cannot contain an error")
			}
			var n Notification
			if err := json.Unmarshal(data, &n); err != nil {
				return Frame{}, err
			}
			if strings.TrimSpace(n.Method) == "" {
				return Frame{}, errors.New("notification method is required")
			}
			if len(n.Params) > 0 && !json.Valid(n.Params) {
				return Frame{}, errors.New("notification params are invalid JSON")
			}
			return Frame{Kind: FrameTypeNotification, Notification: &n}, nil
		default:
			return Frame{}, fmt.Errorf("unknown protocol frame type %q", typ)
		}
	}
	if _, ok := fields["id"]; !ok {
		return Frame{}, errors.New("protocol request/response id is required")
	}
	_, method := fields["method"]
	_, result := fields["result"]
	_, structuredErr := fields["error"]
	if method && !result && !structuredErr {
		return parseRequest(data)
	}
	if !method && (result || structuredErr) {
		return parseResponse(data)
	}
	return Frame{}, errors.New("unknown or malformed protocol frame shape")
}

func parseRequest(data []byte) (Frame, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return Frame{}, fmt.Errorf("malformed request frame: %w", err)
	}
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Method) == "" {
		return Frame{}, errors.New("request id and method are required")
	}
	if len(req.Params) > 0 && !json.Valid(req.Params) {
		return Frame{}, errors.New("request params are invalid JSON")
	}
	return Frame{Kind: FrameTypeRequest, Request: &req}, nil
}
func parseResponse(data []byte) (Frame, error) {
	var res Response
	if err := json.Unmarshal(data, &res); err != nil {
		return Frame{}, fmt.Errorf("malformed response frame: %w", err)
	}
	if strings.TrimSpace(res.ID) == "" {
		return Frame{}, errors.New("response id is required")
	}
	if res.Error == nil && len(res.Result) == 0 {
		return Frame{}, errors.New("response must contain result or error")
	}
	if res.Error != nil && len(res.Result) > 0 {
		return Frame{}, errors.New("response cannot contain both result and error")
	}
	if res.Error != nil && res.Error.Code == "" {
		return Frame{}, errors.New("structured error code is required")
	}
	return Frame{Kind: FrameTypeResponse, Response: &res}, nil
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(frame)+len(part) > MaxFrameBytes+1 {
			return nil, fmt.Errorf("protocol frame exceeds %d bytes", MaxFrameBytes)
		}
		frame = append(frame, part...)
		if err == nil {
			frame = frame[:len(frame)-1]
			if len(frame) == 0 {
				return nil, errors.New("empty protocol frame")
			}
			return frame, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(frame) == 0 {
				return nil, io.EOF
			}
			return nil, errors.New("unterminated protocol frame")
		}
		return nil, err
	}
}
func writeFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > MaxFrameBytes {
		return fmt.Errorf("protocol frame exceeds %d bytes", MaxFrameBytes)
	}
	data = append(data, '\n')
	for len(data) > 0 {
		n, e := w.Write(data)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// DecodeObject decodes one JSON object while preserving exact numbers in
// RawMessage fields. It rejects null, arrays, and trailing JSON values.
func DecodeObject(raw json.RawMessage, dst any) error {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 || trim[0] != '{' {
		return errors.New("parameters must be a JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(trim))
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
