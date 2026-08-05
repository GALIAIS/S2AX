package invocationarchive

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const recordColumns = `
id,created_at,completed_at,expires_at,config_version,mode,transport,websocket_turn,
user_id,user_label,api_key_id,api_key_name,group_id,group_name,
request_id,client_request_id,method,request_path,model,client_ip,user_agent,
request_content_type,response_content_type,http_status,
request_total_bytes,request_captured_bytes,request_truncated,request_status,
response_total_bytes,response_captured_bytes,response_truncated,response_status,outcome`

const (
	defaultPayloadChunkBytes = 256 << 10
	maxPayloadChunkBytes     = 1 << 20
	archivePayloadBlockBytes = maxPayloadChunkBytes
	// ponytail: one legacy record per maintenance pass caps catch-up CPU and memory; raise only when a measured backlog needs faster migration.
	archiveLegacyShardBatchSize = 1
)

type persistedPayload struct {
	status                  string
	ciphertext              string
	compression             string
	storedBytes             int64
	uncompressedStoredBytes int64
	chunked                 bool
	blocks                  []storedPayloadBlock
}

type storedPayloadBlock struct {
	recordID                int64
	slot                    PayloadSlot
	index                   int
	offset                  int64
	capturedBytes           int64
	ciphertext              string
	compression             string
	storedBytes             int64
	uncompressedStoredBytes int64
}

func (s *Service) persistCandidate(ctx context.Context, candidate archiveCandidate) error {
	if s == nil || s.db == nil {
		return errors.New("invocation archive database unavailable")
	}
	request, requestErr := prepareCapturedPayloadStorage(s.encryptor, candidate.request)
	response, responseErr := prepareCapturedPayloadStorage(s.encryptor, candidate.response)
	if requestErr != nil {
		request.status = "encryption_failed"
	}
	if responseErr != nil {
		response.status = "encryption_failed"
	}

	metadata := candidate.identity
	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	var recordID int64
	err = transaction.QueryRowContext(ctx, `
		INSERT INTO invocation_archive_records(
			created_at,completed_at,expires_at,config_version,mode,transport,websocket_turn,
			user_id,user_label,api_key_id,api_key_name,group_id,group_name,
			request_id,client_request_id,method,request_path,model,client_ip,user_agent,
			request_content_type,response_content_type,http_status,
			request_total_bytes,request_captured_bytes,request_truncated,request_status,request_ciphertext,
			request_compression,request_stored_bytes,request_uncompressed_stored_bytes,request_chunked,
			response_total_bytes,response_captured_bytes,response_truncated,response_status,response_ciphertext,
			response_compression,response_stored_bytes,response_uncompressed_stored_bytes,response_chunked,outcome
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,$11,$12,$13,
			$14,$15,$16,$17,$18,$19,$20,
			$21,$22,$23,
			$24,$25,$26,$27,$28,
			$29,$30,$31,$32,
			$33,$34,$35,$36,$37,
			$38,$39,$40,$41,$42)
		RETURNING id`,
		candidate.createdAt, candidate.completedAt, candidate.expiresAt, candidate.configVersion, candidate.mode, candidate.transport, candidate.websocketTurn,
		metadata.userID, metadata.userLabel, metadata.apiKeyID, metadata.apiKeyName, metadata.groupID, metadata.groupName,
		candidate.requestID, candidate.clientRequestID, candidate.method, candidate.path, candidateModel(candidate), candidate.clientIP, candidate.userAgent,
		candidate.request.contentType, candidate.response.contentType, candidate.httpStatus,
		candidate.request.total, int64(len(candidate.request.bytes)), candidate.request.truncated, request.status, request.ciphertext,
		request.compression, request.storedBytes, request.uncompressedStoredBytes, request.chunked,
		candidate.response.total, int64(len(candidate.response.bytes)), candidate.response.truncated, response.status, response.ciphertext,
		response.compression, response.storedBytes, response.uncompressedStoredBytes, response.chunked, candidate.outcome,
	).Scan(&recordID)
	if err != nil {
		return err
	}
	if err := insertPayloadBlocks(ctx, transaction, recordID, PayloadSlotRequest, request.blocks); err != nil {
		return err
	}
	if err := insertPayloadBlocks(ctx, transaction, recordID, PayloadSlotResponse, response.blocks); err != nil {
		return err
	}
	return transaction.Commit()
}

func candidateModel(candidate archiveCandidate) string {
	if candidate.model != "" {
		return trimText(candidate.model, 255)
	}
	return extractModel(candidate.request.bytes, candidate.path)
}

type archiveRecordIdentity struct {
	userID     any
	userLabel  string
	apiKeyID   any
	apiKeyName string
	groupID    any
	groupName  string
}

func archiveIdentity(apiKey *service.APIKey) archiveRecordIdentity {
	if apiKey == nil {
		return archiveRecordIdentity{}
	}
	identity := archiveRecordIdentity{
		userID: nullableID(apiKey.UserID), apiKeyID: nullableID(apiKey.ID), apiKeyName: trimText(apiKey.Name, 255),
	}
	if apiKey.User != nil {
		identity.userLabel = trimText(apiKey.User.Email, 255)
		if apiKey.User.Username != "" {
			identity.userLabel = trimText(apiKey.User.Username+" <"+apiKey.User.Email+">", 255)
		}
	}
	if apiKey.GroupID != nil && *apiKey.GroupID > 0 {
		identity.groupID = *apiKey.GroupID
	}
	if apiKey.Group != nil {
		identity.groupName = trimText(apiKey.Group.Name, 255)
	}
	return identity
}

func protectCapturedPayload(encryptor service.SecretEncryptor, payload capturedPayload) (string, string, error) {
	if payload.status != "captured" {
		return "", payload.status, nil
	}
	ciphertext, _, err := protectPayloadEnvelope(encryptor, newPayloadEnvelope(payload.bytes, payload.frames, payload.framesTruncated))
	if err != nil {
		return "", "encryption_failed", err
	}
	return ciphertext, payload.status, nil
}

func prepareCapturedPayloadStorage(encryptor service.SecretEncryptor, payload capturedPayload) (persistedPayload, error) {
	storage := persistedPayload{status: payload.status, compression: payloadCompressionNone}
	if payload.status != "captured" {
		return storage, nil
	}
	blocks, err := protectPayloadBlocks(encryptor, newPayloadEnvelope(payload.bytes, payload.frames, payload.framesTruncated))
	if err != nil {
		storage.status = "encryption_failed"
		return storage, err
	}
	storage.chunked = len(blocks) > 0
	storage.blocks = blocks
	for _, block := range blocks {
		storage.storedBytes += block.storedBytes
		storage.uncompressedStoredBytes += block.uncompressedStoredBytes
		if block.compression == payloadCompressionGzip {
			storage.compression = payloadCompressionGzip
		}
	}
	return storage, nil
}

func protectPayloadBlocks(encryptor service.SecretEncryptor, source payloadEnvelope) ([]storedPayloadBlock, error) {
	compression := normalizePayloadCompression(source.Compression)
	if compression == "" {
		return nil, ErrPayloadUnavailable
	}
	raw, err := decodePayloadEnvelope(source)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if source.Encoding != "utf8" && source.Encoding != "base64" {
		return nil, ErrPayloadUnavailable
	}
	blocks := make([]storedPayloadBlock, 0, (len(raw)+archivePayloadBlockBytes-1)/archivePayloadBlockBytes)
	for start := 0; start < len(raw); {
		end, err := payloadBlockEnd(raw, source.Encoding, start)
		if err != nil {
			return nil, err
		}
		frames, err := payloadBlockFrames(source.Frames, int64(len(raw)), int64(start), int64(end))
		if err != nil {
			return nil, err
		}
		envelope := newPayloadEnvelopeWithEncoding(raw[start:end], source.Encoding, frames, source.FramesTruncated)
		uncompressedCiphertext, _, err := protectPayloadEnvelope(encryptor, envelope)
		if err != nil {
			return nil, err
		}
		ciphertext := uncompressedCiphertext
		blockCompression := payloadCompressionNone
		if compression == payloadCompressionGzip {
			compressed, _, err := gzipPayloadEnvelope(envelope)
			if err != nil {
				return nil, err
			}
			compressedCiphertext, _, err := protectPayloadEnvelope(encryptor, compressed)
			if err != nil {
				return nil, err
			}
			if len(compressedCiphertext) < len(uncompressedCiphertext) {
				ciphertext = compressedCiphertext
				blockCompression = payloadCompressionGzip
			}
		}
		blocks = append(blocks, storedPayloadBlock{
			index: len(blocks), offset: int64(start), capturedBytes: int64(end - start), ciphertext: ciphertext,
			compression: blockCompression, storedBytes: int64(len(ciphertext)), uncompressedStoredBytes: int64(len(uncompressedCiphertext)),
		})
		start = end
	}
	return blocks, nil
}

func payloadBlockEnd(payload []byte, encoding string, start int) (int, error) {
	end := start + archivePayloadBlockBytes
	if end > len(payload) {
		end = len(payload)
	}
	if encoding != "utf8" || end == len(payload) {
		return end, nil
	}
	for end > start && !utf8.Valid(payload[start:end]) {
		end--
	}
	if end == start {
		return 0, ErrPayloadUnavailable
	}
	return end, nil
}

func payloadBlockFrames(frames []payloadFrameEnvelope, payloadSize, start, end int64) ([]capturedFrame, error) {
	if len(frames) == 0 {
		return nil, nil
	}
	if len(frames) > maxWebSocketFrameMetadata || start < 0 || end < start || end > payloadSize {
		return nil, ErrPayloadUnavailable
	}
	result := make([]capturedFrame, 0, len(frames))
	for index, frame := range frames {
		if frame.Offset < 0 || frame.CapturedBytes < 0 || frame.TotalBytes < frame.CapturedBytes || frame.Offset > payloadSize {
			return nil, ErrPayloadUnavailable
		}
		frameEnd := frame.Offset + frame.CapturedBytes
		if frameEnd < frame.Offset || frameEnd > payloadSize {
			return nil, ErrPayloadUnavailable
		}
		overlapStart := frame.Offset
		if overlapStart < start {
			overlapStart = start
		}
		overlapEnd := frameEnd
		if overlapEnd > end {
			overlapEnd = end
		}
		if overlapEnd <= overlapStart {
			continue
		}
		sequence := frame.Sequence
		if sequence < 1 {
			sequence = index + 1
		}
		result = append(result, capturedFrame{
			sequence: sequence, kind: frame.Kind, occurredAt: frame.OccurredAt, offset: overlapStart - start,
			totalBytes: frame.TotalBytes, capturedBytes: overlapEnd - overlapStart,
			truncated: frame.Truncated || overlapStart != frame.Offset || overlapEnd != frameEnd,
		})
	}
	return result, nil
}

func insertPayloadBlocks(ctx context.Context, transaction *sql.Tx, recordID int64, slot PayloadSlot, blocks []storedPayloadBlock) error {
	for _, block := range blocks {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO invocation_archive_payload_blocks(
				record_id,slot,block_index,byte_offset,captured_bytes,ciphertext,compression,stored_bytes,uncompressed_stored_bytes
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			recordID, slot, block.index, block.offset, block.capturedBytes, block.ciphertext,
			block.compression, block.storedBytes, block.uncompressedStoredBytes,
		); err != nil {
			return err
		}
	}
	return nil
}

type storedPayload struct {
	recordID      int64
	expiresAt     time.Time
	contentType   string
	status        string
	totalBytes    int64
	capturedBytes int64
	truncated     bool
	ciphertext    string
	chunked       bool
}

func (s *Service) getStoredPayload(ctx context.Context, id int64, slot PayloadSlot) (storedPayload, error) {
	if s == nil || s.db == nil {
		return storedPayload{}, errors.New("invocation archive database unavailable")
	}
	if id <= 0 {
		return storedPayload{}, ErrRecordNotFound
	}
	var query string
	switch slot {
	case PayloadSlotRequest:
		query = `SELECT id,expires_at,request_content_type,request_status,request_total_bytes,request_captured_bytes,request_truncated,request_ciphertext,request_chunked
			FROM invocation_archive_records WHERE id=$1`
	case PayloadSlotResponse:
		query = `SELECT id,expires_at,response_content_type,response_status,response_total_bytes,response_captured_bytes,response_truncated,response_ciphertext,response_chunked
			FROM invocation_archive_records WHERE id=$1`
	default:
		return storedPayload{}, ErrPayloadRangeInvalid
	}
	var payload storedPayload
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&payload.recordID, &payload.expiresAt, &payload.contentType, &payload.status,
		&payload.totalBytes, &payload.capturedBytes, &payload.truncated, &payload.ciphertext, &payload.chunked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPayload{}, ErrRecordNotFound
	}
	return payload, err
}

// RevealPayloadChunk returns a bounded segment of one encrypted payload. New
// archives decrypt only the encrypted storage block that contains the range.
func (s *Service) RevealPayloadChunk(ctx context.Context, id, adminID int64, slot PayloadSlot, offset int64, limit int, clientIP, userAgent string) (*PayloadChunk, error) {
	if adminID <= 0 {
		return nil, infraerrors.Forbidden("invocation_archive_admin_required", "管理员身份无效")
	}
	payload, err := s.getStoredPayload(ctx, id, slot)
	if err != nil {
		return nil, err
	}
	if !s.activeConfig().DirectViewEnabled {
		_ = s.recordAccess(ctx, id, adminID, "", "direct_view_disabled", clientIP, userAgent)
		return nil, ErrDirectViewDisabled
	}
	if !time.Now().UTC().Before(payload.expiresAt) {
		_ = s.recordAccess(ctx, id, adminID, "", "expired", clientIP, userAgent)
		return nil, ErrPayloadExpired
	}
	var view PayloadView
	var nextOffset int64
	if payload.chunked {
		view, nextOffset, err = s.revealPayloadBlockSegment(ctx, payload, slot, offset, limit)
	} else {
		view, nextOffset, err = revealPayloadSegment(s.encryptor, payload, offset, limit)
	}
	if err != nil {
		_ = s.recordAccess(ctx, id, adminID, "", "decrypt_failed", clientIP, userAgent)
		if errors.Is(err, ErrPayloadRangeInvalid) {
			return nil, err
		}
		return nil, ErrPayloadUnavailable
	}
	outcome := "revealed"
	if !view.Available {
		outcome = "unavailable"
	}
	if err := s.recordAccess(ctx, id, adminID, fmt.Sprintf("%s:%d-%d", slot, view.Offset, nextOffset), outcome, clientIP, userAgent); err != nil {
		return nil, fmt.Errorf("record invocation archive payload view before response: %w", err)
	}
	return &PayloadChunk{RecordID: id, Slot: slot, Payload: view, NextOffset: nextOffset}, nil
}

func (s *Service) revealPayloadBlockSegment(ctx context.Context, stored storedPayload, slot PayloadSlot, offset int64, limit int) (PayloadView, int64, error) {
	view := PayloadView{
		Status: stored.status, ContentType: stored.contentType, TotalBytes: stored.totalBytes,
		CapturedBytes: stored.capturedBytes, Truncated: stored.truncated, Complete: true,
	}
	if stored.status != "captured" {
		return view, 0, nil
	}
	if limit < 1 || limit > maxPayloadChunkBytes || offset < 0 || offset >= stored.capturedBytes {
		return view, 0, ErrPayloadRangeInvalid
	}
	block, found, err := s.getPayloadBlock(ctx, stored.recordID, slot, offset)
	if err != nil {
		return view, 0, err
	}
	if !found {
		return view, 0, ErrPayloadUnavailable
	}
	envelope, raw, err := decryptPayloadEnvelope(s.encryptor, block.ciphertext)
	if err != nil || int64(len(raw)) != block.capturedBytes || normalizePayloadCompression(envelope.Compression) != normalizePayloadCompression(block.compression) {
		return view, 0, ErrPayloadUnavailable
	}
	start, end, err := payloadSegmentBounds(raw, envelope.Encoding, offset-block.offset, limit)
	if err != nil {
		return view, 0, err
	}
	frames, err := revealPayloadFrames(raw, envelope.Frames, start, end)
	if err != nil {
		return view, 0, err
	}
	for index := range frames {
		frames[index].Offset += block.offset
	}
	nextOffset := block.offset + end
	view.Available = true
	view.Encoding = envelope.Encoding
	view.Compression = normalizePayloadCompression(envelope.Compression)
	view.Data = encodePayloadData(raw[start:end], envelope.Encoding)
	view.Offset = block.offset + start
	view.LoadedBytes = end - start
	view.Complete = nextOffset >= stored.capturedBytes
	view.Frames = frames
	view.FramesTruncated = envelope.FramesTruncated
	return view, nextOffset, nil
}

func (s *Service) getPayloadBlock(ctx context.Context, recordID int64, slot PayloadSlot, offset int64) (storedPayloadBlock, bool, error) {
	var block storedPayloadBlock
	err := s.db.QueryRowContext(ctx, `
		SELECT record_id,slot,block_index,byte_offset,captured_bytes,ciphertext,compression,stored_bytes,uncompressed_stored_bytes
		FROM invocation_archive_payload_blocks
		WHERE record_id=$1 AND slot=$2 AND byte_offset <= $3 AND byte_offset + captured_bytes > $3
		ORDER BY byte_offset DESC
		LIMIT 1`, recordID, slot, offset).Scan(
		&block.recordID, &block.slot, &block.index, &block.offset, &block.capturedBytes,
		&block.ciphertext, &block.compression, &block.storedBytes, &block.uncompressedStoredBytes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPayloadBlock{}, false, nil
	}
	return block, err == nil, err
}

func (s *Service) listPayloadBlocks(ctx context.Context, recordID int64, slot PayloadSlot) ([]storedPayloadBlock, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT record_id,slot,block_index,byte_offset,captured_bytes,ciphertext,compression,stored_bytes,uncompressed_stored_bytes
		FROM invocation_archive_payload_blocks
		WHERE record_id=$1 AND slot=$2
		ORDER BY block_index ASC`, recordID, slot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	blocks := make([]storedPayloadBlock, 0)
	for rows.Next() {
		var block storedPayloadBlock
		if err := rows.Scan(
			&block.recordID, &block.slot, &block.index, &block.offset, &block.capturedBytes,
			&block.ciphertext, &block.compression, &block.storedBytes, &block.uncompressedStoredBytes,
		); err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, rows.Err()
}

func (s *Service) revealStoredPayload(ctx context.Context, stored storedPayload, slot PayloadSlot) (PayloadView, error) {
	if !stored.chunked {
		return revealPayload(s.encryptor, stored.ciphertext, stored.contentType, stored.status, stored.totalBytes, stored.capturedBytes, stored.truncated)
	}
	view := PayloadView{
		Status: stored.status, ContentType: stored.contentType, TotalBytes: stored.totalBytes,
		CapturedBytes: stored.capturedBytes, Truncated: stored.truncated, Complete: true,
	}
	if stored.status != "captured" {
		return view, nil
	}
	blocks, err := s.listPayloadBlocks(ctx, stored.recordID, slot)
	if err != nil {
		return view, err
	}
	if len(blocks) == 0 {
		return view, ErrPayloadUnavailable
	}
	var body bytes.Buffer
	frames := make([]PayloadFrameView, 0)
	encoding := ""
	compression := payloadCompressionNone
	expectedOffset := int64(0)
	framesTruncated := false
	for _, block := range blocks {
		if block.offset != expectedOffset {
			return view, ErrPayloadUnavailable
		}
		envelope, raw, err := decryptPayloadEnvelope(s.encryptor, block.ciphertext)
		if err != nil || int64(len(raw)) != block.capturedBytes || normalizePayloadCompression(envelope.Compression) != normalizePayloadCompression(block.compression) {
			return view, ErrPayloadUnavailable
		}
		if encoding == "" {
			encoding = envelope.Encoding
		} else if encoding != envelope.Encoding {
			return view, ErrPayloadUnavailable
		}
		blockFrames, err := revealPayloadFrames(raw, envelope.Frames, 0, int64(len(raw)))
		if err != nil {
			return view, err
		}
		for index := range blockFrames {
			blockFrames[index].Offset += block.offset
		}
		frames = append(frames, blockFrames...)
		framesTruncated = framesTruncated || envelope.FramesTruncated
		if normalizePayloadCompression(envelope.Compression) == payloadCompressionGzip {
			compression = payloadCompressionGzip
		}
		if _, err := body.Write(raw); err != nil {
			return view, err
		}
		expectedOffset += int64(len(raw))
	}
	if expectedOffset != stored.capturedBytes || expectedOffset > maxCaptureBytes {
		return view, ErrPayloadUnavailable
	}
	view.Available = true
	view.Encoding = encoding
	view.Compression = compression
	view.Data = encodePayloadData(body.Bytes(), encoding)
	view.LoadedBytes = expectedOffset
	view.Frames = frames
	view.FramesTruncated = framesTruncated
	return view, nil
}

func revealPayloadSegment(encryptor service.SecretEncryptor, stored storedPayload, offset int64, limit int) (PayloadView, int64, error) {
	view := PayloadView{
		Status: stored.status, ContentType: stored.contentType, TotalBytes: stored.totalBytes,
		CapturedBytes: stored.capturedBytes, Truncated: stored.truncated, Complete: true,
	}
	if strings.TrimSpace(stored.ciphertext) == "" || stored.status != "captured" {
		return view, 0, nil
	}
	if limit < 1 || limit > maxPayloadChunkBytes || offset < 0 {
		return view, 0, ErrPayloadRangeInvalid
	}
	envelope, raw, err := decryptPayloadEnvelope(encryptor, stored.ciphertext)
	if err != nil {
		return view, 0, err
	}
	start, end, err := payloadSegmentBounds(raw, envelope.Encoding, offset, limit)
	if err != nil {
		return view, 0, err
	}
	frames, err := revealPayloadFrames(raw, envelope.Frames, start, end)
	if err != nil {
		return view, 0, err
	}
	view.Available = true
	view.Encoding = envelope.Encoding
	view.Compression = normalizePayloadCompression(envelope.Compression)
	view.Data = encodePayloadData(raw[start:end], envelope.Encoding)
	view.Offset = start
	view.LoadedBytes = end - start
	view.Complete = end == int64(len(raw))
	view.Frames = frames
	view.FramesTruncated = envelope.FramesTruncated
	return view, end, nil
}

func payloadSegmentBounds(payload []byte, encoding string, offset int64, limit int) (int64, int64, error) {
	if offset > int64(len(payload)) {
		return 0, 0, ErrPayloadRangeInvalid
	}
	start := int(offset)
	if encoding == "utf8" && !utf8.Valid(payload[:start]) {
		return 0, 0, ErrPayloadRangeInvalid
	}
	end := start + limit
	if end > len(payload) {
		end = len(payload)
	}
	if encoding == "utf8" {
		for end > start && !utf8.Valid(payload[start:end]) {
			end--
		}
		if end == start && start < len(payload) {
			return 0, 0, ErrPayloadRangeInvalid
		}
	}
	return int64(start), int64(end), nil
}

type compressionPayload struct {
	ciphertext              string
	status                  string
	compression             string
	capturedBytes           int64
	storedBytes             int64
	uncompressedStoredBytes int64
}

type compressionCandidate struct {
	id       int64
	request  compressionPayload
	response compressionPayload
}

type compressionResult struct {
	records    int
	payloads   int
	savedBytes int64
}

func (s *Service) refreshStorageStats(parent context.Context) {
	if s == nil || s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), archiveWriteTimeout)
	defer cancel()
	var stats ArchiveStorageStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*)::bigint,
			(SELECT COUNT(*)::bigint FROM invocation_archive_payload_blocks),
			COALESCE(SUM(request_captured_bytes + response_captured_bytes)::bigint, 0),
			COALESCE(SUM(request_stored_bytes + response_stored_bytes)::bigint, 0),
			COALESCE(SUM(CASE WHEN request_compression = 'gzip' OR response_compression = 'gzip' THEN 1 ELSE 0 END)::bigint, 0),
			COALESCE(SUM(CASE WHEN request_compression = 'gzip' THEN 1 ELSE 0 END + CASE WHEN response_compression = 'gzip' THEN 1 ELSE 0 END)::bigint, 0),
			COALESCE(SUM(GREATEST(request_uncompressed_stored_bytes - request_stored_bytes, 0) + GREATEST(response_uncompressed_stored_bytes - response_stored_bytes, 0))::bigint, 0)
		FROM invocation_archive_records`).Scan(
		&stats.RecordCount, &stats.BlockCount, &stats.CapturedBytes, &stats.PayloadBytes, &stats.CompressedRecords,
		&stats.CompressedPayloads, &stats.SavedBytes,
	)
	if err != nil {
		s.setStorageError(err)
		return
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(pg_total_relation_size('invocation_archive_records'::regclass), 0)
			+ COALESCE(pg_total_relation_size('invocation_archive_payload_blocks'::regclass), 0)`).Scan(&stats.DatabaseBytes); err != nil {
		s.setStorageError(err)
		return
	}
	now := time.Now().UTC()
	stats.UpdatedAt = &now
	s.stateMu.Lock()
	s.storage = stats
	s.lastStorageError = ""
	s.lastStorageErrorAt = nil
	s.stateMu.Unlock()
}

func (s *Service) shardLegacyPayloads(parent context.Context) {
	if s == nil || s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), archiveMaintenanceTimeout)
	defer cancel()
	if _, err := s.shardLegacyPayloadBatch(ctx, time.Now().UTC(), archiveLegacyShardBatchSize); err != nil {
		s.setStorageError(fmt.Errorf("migrate legacy archive payload blocks: %w", err))
	}
}

func (s *Service) shardLegacyPayloadBatch(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 {
		return 0, nil
	}
	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = transaction.Rollback() }()
	rows, err := transaction.QueryContext(ctx, `
		SELECT id,
			request_ciphertext,request_status,request_chunked,request_captured_bytes,
			response_ciphertext,response_status,response_chunked,response_captured_bytes
		FROM invocation_archive_records
		WHERE expires_at > $1
			AND (
				(NOT request_chunked AND request_status='captured' AND request_ciphertext <> '' AND request_captured_bytes > $2)
				OR (NOT response_chunked AND response_status='captured' AND response_ciphertext <> '' AND response_captured_bytes > $2)
			)
		ORDER BY created_at ASC,id ASC
		LIMIT $3 FOR UPDATE SKIP LOCKED`, now, archivePayloadBlockBytes, limit)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		id                    int64
		requestCiphertext     string
		requestStatus         string
		requestChunked        bool
		requestCapturedBytes  int64
		responseCiphertext    string
		responseStatus        string
		responseChunked       bool
		responseCapturedBytes int64
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.id,
			&item.requestCiphertext, &item.requestStatus, &item.requestChunked, &item.requestCapturedBytes,
			&item.responseCiphertext, &item.responseStatus, &item.responseChunked, &item.responseCapturedBytes,
		); err != nil {
			_ = rows.Close()
			return 0, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, candidate := range candidates {
		if !candidate.requestChunked && candidate.requestStatus == "captured" && candidate.requestCapturedBytes > archivePayloadBlockBytes {
			if err := s.shardLegacyPayload(ctx, transaction, candidate.id, PayloadSlotRequest, candidate.requestCiphertext); err != nil {
				return 0, err
			}
		}
		if !candidate.responseChunked && candidate.responseStatus == "captured" && candidate.responseCapturedBytes > archivePayloadBlockBytes {
			if err := s.shardLegacyPayload(ctx, transaction, candidate.id, PayloadSlotResponse, candidate.responseCiphertext); err != nil {
				return 0, err
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	return len(candidates), nil
}

func (s *Service) shardLegacyPayload(ctx context.Context, transaction *sql.Tx, recordID int64, slot PayloadSlot, ciphertext string) error {
	envelope, _, err := decryptPayloadEnvelope(s.encryptor, ciphertext)
	if err != nil {
		return err
	}
	blocks, err := protectPayloadBlocks(s.encryptor, envelope)
	if err != nil || len(blocks) == 0 {
		if err != nil {
			return err
		}
		return ErrPayloadUnavailable
	}
	if err := insertPayloadBlocks(ctx, transaction, recordID, slot, blocks); err != nil {
		return err
	}
	compression, storedBytes, uncompressedStoredBytes := summarizePayloadBlocks(blocks)
	var query string
	switch slot {
	case PayloadSlotRequest:
		query = `UPDATE invocation_archive_records
			SET request_ciphertext='',request_chunked=TRUE,request_compression=$2,request_stored_bytes=$3,request_uncompressed_stored_bytes=$4
			WHERE id=$1`
	case PayloadSlotResponse:
		query = `UPDATE invocation_archive_records
			SET response_ciphertext='',response_chunked=TRUE,response_compression=$2,response_stored_bytes=$3,response_uncompressed_stored_bytes=$4
			WHERE id=$1`
	default:
		return ErrPayloadRangeInvalid
	}
	_, err = transaction.ExecContext(ctx, query, recordID, compression, storedBytes, uncompressedStoredBytes)
	return err
}

func summarizePayloadBlocks(blocks []storedPayloadBlock) (string, int64, int64) {
	compression := payloadCompressionNone
	var storedBytes, uncompressedStoredBytes int64
	for _, block := range blocks {
		storedBytes += block.storedBytes
		uncompressedStoredBytes += block.uncompressedStoredBytes
		if block.compression == payloadCompressionGzip {
			compression = payloadCompressionGzip
		}
	}
	return compression, storedBytes, uncompressedStoredBytes
}

func (s *Service) maybeCompactArchive(parent context.Context) {
	if s == nil || s.db == nil {
		return
	}
	cfg := s.activeConfig().Compression
	if !cfg.Enabled {
		return
	}
	now := time.Now().UTC()
	s.stateMu.Lock()
	if s.compression.LastCheckedAt != nil && now.Sub(*s.compression.LastCheckedAt) < time.Duration(cfg.IntervalMinutes)*time.Minute {
		s.stateMu.Unlock()
		return
	}
	storage := cloneArchiveStorageStats(s.storage)
	s.compression.Enabled = true
	s.compression.Runs++
	s.compression.LastCheckedAt = &now
	s.stateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), archiveMaintenanceTimeout)
	defer cancel()
	result, err := s.compactArchivePayloads(ctx, cfg, storage, now)
	if err != nil {
		s.stateMu.Lock()
		s.compression.LastError = trimText(err.Error(), 512)
		s.compression.LastErrorAt = &now
		s.stateMu.Unlock()
		return
	}
	s.stateMu.Lock()
	s.compression.LastError = ""
	s.compression.LastErrorAt = nil
	if result.payloads > 0 {
		s.compression.Records += uint64(result.records)
		s.compression.Payloads += uint64(result.payloads)
		s.compression.SavedBytes += uint64(result.savedBytes)
		s.compression.LastCompressedAt = &now
	}
	s.stateMu.Unlock()
	if result.payloads > 0 {
		s.refreshStorageStats(parent)
	}
}

func (s *Service) compactArchivePayloads(ctx context.Context, cfg CompressionConfig, storage ArchiveStorageStats, now time.Time) (compressionResult, error) {
	legacy, err := s.compactLegacyArchivePayloads(ctx, cfg, storage, now)
	if err != nil {
		return compressionResult{}, err
	}
	blocks, err := s.compactPayloadBlocks(ctx, cfg, storage, now)
	if err != nil {
		return compressionResult{}, err
	}
	return compressionResult{
		records: legacy.records + blocks.records, payloads: legacy.payloads + blocks.payloads,
		savedBytes: legacy.savedBytes + blocks.savedBytes,
	}, nil
}

func (s *Service) compactLegacyArchivePayloads(ctx context.Context, cfg CompressionConfig, storage ArchiveStorageStats, now time.Time) (compressionResult, error) {
	ageDue := cfg.AfterHours > 0
	ageCutoff := now.Add(-time.Duration(cfg.AfterHours) * time.Hour)
	pressureDue := (cfg.TriggerBytes > 0 && storage.PayloadBytes >= cfg.TriggerBytes) || (cfg.TriggerRecords > 0 && storage.RecordCount >= int64(cfg.TriggerRecords))
	if !ageDue && !pressureDue {
		return compressionResult{}, nil
	}
	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return compressionResult{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	rows, err := transaction.QueryContext(ctx, `
		SELECT id,
			request_ciphertext,request_status,request_compression,request_captured_bytes,request_stored_bytes,request_uncompressed_stored_bytes,
			response_ciphertext,response_status,response_compression,response_captured_bytes,response_stored_bytes,response_uncompressed_stored_bytes
		FROM invocation_archive_records
		WHERE expires_at > $1
			AND ((NOT request_chunked AND request_compression = 'none') OR (NOT response_chunked AND response_compression = 'none'))
			AND (( $2::boolean AND created_at <= $3) OR $4::boolean)
			AND (compression_checked_at IS NULL OR compression_checked_at <= $5)
			AND (
				(NOT request_chunked AND request_compression = 'none' AND request_status = 'captured' AND request_captured_bytes >= $6)
				OR (NOT response_chunked AND response_compression = 'none' AND response_status = 'captured' AND response_captured_bytes >= $6)
			)
		ORDER BY created_at ASC,id ASC
		LIMIT $7 FOR UPDATE SKIP LOCKED`,
		now, ageDue, ageCutoff, pressureDue, now.Add(-time.Duration(cfg.IntervalMinutes)*time.Minute), cfg.MinBytes, cfg.BatchSize,
	)
	if err != nil {
		return compressionResult{}, err
	}
	candidates := make([]compressionCandidate, 0, cfg.BatchSize)
	for rows.Next() {
		var candidate compressionCandidate
		if err := rows.Scan(
			&candidate.id,
			&candidate.request.ciphertext, &candidate.request.status, &candidate.request.compression, &candidate.request.capturedBytes, &candidate.request.storedBytes, &candidate.request.uncompressedStoredBytes,
			&candidate.response.ciphertext, &candidate.response.status, &candidate.response.compression, &candidate.response.capturedBytes, &candidate.response.storedBytes, &candidate.response.uncompressedStoredBytes,
		); err != nil {
			_ = rows.Close()
			return compressionResult{}, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Close(); err != nil {
		return compressionResult{}, err
	}
	if err := rows.Err(); err != nil {
		return compressionResult{}, err
	}
	result := compressionResult{}
	for _, candidate := range candidates {
		request, requestChanged, requestSaved, requestErr := compactStoredPayload(s.encryptor, candidate.request, cfg.MinBytes)
		response, responseChanged, responseSaved, responseErr := compactStoredPayload(s.encryptor, candidate.response, cfg.MinBytes)
		if requestErr != nil || responseErr != nil {
			if _, err := transaction.ExecContext(ctx, `UPDATE invocation_archive_records SET compression_checked_at=$2 WHERE id=$1`, candidate.id, now); err != nil {
				return compressionResult{}, err
			}
			continue
		}
		if !requestChanged && !responseChanged {
			if _, err := transaction.ExecContext(ctx, `UPDATE invocation_archive_records SET compression_checked_at=$2 WHERE id=$1`, candidate.id, now); err != nil {
				return compressionResult{}, err
			}
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
			UPDATE invocation_archive_records
			SET request_ciphertext=$2,request_compression=$3,request_stored_bytes=$4,request_uncompressed_stored_bytes=$5,
				response_ciphertext=$6,response_compression=$7,response_stored_bytes=$8,response_uncompressed_stored_bytes=$9,
				compression_checked_at=$10,compressed_at=$10
			WHERE id=$1`,
			candidate.id,
			request.ciphertext, request.compression, request.storedBytes, request.uncompressedStoredBytes,
			response.ciphertext, response.compression, response.storedBytes, response.uncompressedStoredBytes,
			now,
		); err != nil {
			return compressionResult{}, err
		}
		result.records++
		if requestChanged {
			result.payloads++
			result.savedBytes += requestSaved
		}
		if responseChanged {
			result.payloads++
			result.savedBytes += responseSaved
		}
	}
	if err := transaction.Commit(); err != nil {
		return compressionResult{}, err
	}
	return result, nil
}

func (s *Service) compactPayloadBlocks(ctx context.Context, cfg CompressionConfig, storage ArchiveStorageStats, now time.Time) (compressionResult, error) {
	ageDue := cfg.AfterHours > 0
	ageCutoff := now.Add(-time.Duration(cfg.AfterHours) * time.Hour)
	pressureDue := (cfg.TriggerBytes > 0 && storage.PayloadBytes >= cfg.TriggerBytes) || (cfg.TriggerRecords > 0 && storage.RecordCount >= int64(cfg.TriggerRecords))
	if !ageDue && !pressureDue {
		return compressionResult{}, nil
	}
	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return compressionResult{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	rows, err := transaction.QueryContext(ctx, `
		SELECT b.record_id,b.slot,b.block_index,b.byte_offset,b.captured_bytes,b.ciphertext,b.compression,b.stored_bytes,b.uncompressed_stored_bytes
		FROM invocation_archive_payload_blocks b
		JOIN invocation_archive_records r ON r.id=b.record_id
		WHERE r.expires_at > $1
			AND b.compression = 'none'
			AND (($2::boolean AND r.created_at <= $3) OR $4::boolean)
			AND (b.compression_checked_at IS NULL OR b.compression_checked_at <= $5)
			AND b.captured_bytes >= $6
		ORDER BY r.created_at ASC,b.record_id ASC,b.slot ASC,b.block_index ASC
		LIMIT $7 FOR UPDATE OF b SKIP LOCKED`,
		now, ageDue, ageCutoff, pressureDue, now.Add(-time.Duration(cfg.IntervalMinutes)*time.Minute), cfg.MinBytes, cfg.BatchSize,
	)
	if err != nil {
		return compressionResult{}, err
	}
	blocks := make([]storedPayloadBlock, 0, cfg.BatchSize)
	for rows.Next() {
		var block storedPayloadBlock
		if err := rows.Scan(
			&block.recordID, &block.slot, &block.index, &block.offset, &block.capturedBytes,
			&block.ciphertext, &block.compression, &block.storedBytes, &block.uncompressedStoredBytes,
		); err != nil {
			_ = rows.Close()
			return compressionResult{}, err
		}
		blocks = append(blocks, block)
	}
	if err := rows.Close(); err != nil {
		return compressionResult{}, err
	}
	if err := rows.Err(); err != nil {
		return compressionResult{}, err
	}
	result := compressionResult{}
	changedRecords := make(map[int64]struct{})
	changedSlots := make(map[payloadBlockAggregateKey]struct{})
	for _, block := range blocks {
		payload, changed, saved, compactErr := compactStoredPayload(s.encryptor, compressionPayload{
			ciphertext: block.ciphertext, status: "captured", compression: block.compression,
			capturedBytes: block.capturedBytes, storedBytes: block.storedBytes, uncompressedStoredBytes: block.uncompressedStoredBytes,
		}, cfg.MinBytes)
		if compactErr != nil || !changed {
			if _, err := transaction.ExecContext(ctx, `
				UPDATE invocation_archive_payload_blocks SET compression_checked_at=$4
				WHERE record_id=$1 AND slot=$2 AND block_index=$3`, block.recordID, block.slot, block.index, now); err != nil {
				return compressionResult{}, err
			}
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
			UPDATE invocation_archive_payload_blocks
			SET ciphertext=$4,compression=$5,stored_bytes=$6,uncompressed_stored_bytes=$7,compression_checked_at=$8,compressed_at=$8
			WHERE record_id=$1 AND slot=$2 AND block_index=$3`,
			block.recordID, block.slot, block.index, payload.ciphertext, payload.compression, payload.storedBytes, payload.uncompressedStoredBytes, now,
		); err != nil {
			return compressionResult{}, err
		}
		changedRecords[block.recordID] = struct{}{}
		changedSlots[payloadBlockAggregateKey{recordID: block.recordID, slot: block.slot}] = struct{}{}
		result.payloads++
		result.savedBytes += saved
	}
	for key := range changedSlots {
		if err := refreshPayloadBlockAggregate(ctx, transaction, key.recordID, key.slot); err != nil {
			return compressionResult{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return compressionResult{}, err
	}
	result.records = len(changedRecords)
	return result, nil
}

type payloadBlockAggregateKey struct {
	recordID int64
	slot     PayloadSlot
}

func refreshPayloadBlockAggregate(ctx context.Context, transaction *sql.Tx, recordID int64, slot PayloadSlot) error {
	var storedBytes, uncompressedStoredBytes int64
	var anyCompressed bool
	if err := transaction.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(stored_bytes), 0),COALESCE(SUM(uncompressed_stored_bytes), 0),COALESCE(BOOL_OR(compression='gzip'), FALSE)
		FROM invocation_archive_payload_blocks
		WHERE record_id=$1 AND slot=$2`, recordID, slot).Scan(&storedBytes, &uncompressedStoredBytes, &anyCompressed); err != nil {
		return err
	}
	compression := payloadCompressionNone
	if anyCompressed {
		compression = payloadCompressionGzip
	}
	var query string
	switch slot {
	case PayloadSlotRequest:
		query = `UPDATE invocation_archive_records
			SET request_compression=$2,request_stored_bytes=$3,request_uncompressed_stored_bytes=$4
			WHERE id=$1`
	case PayloadSlotResponse:
		query = `UPDATE invocation_archive_records
			SET response_compression=$2,response_stored_bytes=$3,response_uncompressed_stored_bytes=$4
			WHERE id=$1`
	default:
		return ErrPayloadRangeInvalid
	}
	_, err := transaction.ExecContext(ctx, query, recordID, compression, storedBytes, uncompressedStoredBytes)
	return err
}

func compactStoredPayload(encryptor service.SecretEncryptor, payload compressionPayload, minBytes int) (compressionPayload, bool, int64, error) {
	payload.compression = normalizePayloadCompression(payload.compression)
	if payload.compression == "" {
		return payload, false, 0, ErrPayloadUnavailable
	}
	if payload.status != "captured" || payload.ciphertext == "" || payload.compression != payloadCompressionNone || payload.capturedBytes < int64(minBytes) {
		return payload, false, 0, nil
	}
	if payload.storedBytes == 0 {
		payload.storedBytes = int64(len(payload.ciphertext))
	}
	if payload.uncompressedStoredBytes == 0 {
		payload.uncompressedStoredBytes = payload.storedBytes
	}
	envelope, _, err := decryptPayloadEnvelope(encryptor, payload.ciphertext)
	if err != nil {
		return payload, false, 0, err
	}
	compressed, changed, err := gzipPayloadEnvelope(envelope)
	if err != nil || !changed {
		return payload, false, 0, err
	}
	ciphertext, _, err := protectPayloadEnvelope(encryptor, compressed)
	if err != nil {
		return payload, false, 0, err
	}
	storedBytes := int64(len(ciphertext))
	if storedBytes >= payload.storedBytes {
		return payload, false, 0, nil
	}
	savedBytes := payload.storedBytes - storedBytes
	payload.ciphertext = ciphertext
	payload.compression = payloadCompressionGzip
	payload.storedBytes = storedBytes
	return payload, true, savedBytes, nil
}

func (s *Service) ListRecords(ctx context.Context, filter RecordFilter, page, pageSize int) (*RecordPage, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, infraerrors.BadRequest("invocation_archive_pagination_invalid", "分页参数无效")
	}
	where, args, err := recordWhere(filter)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM invocation_archive_records WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := "SELECT " + recordColumns + " FROM invocation_archive_records WHERE " + where +
		fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Record, 0, pageSize)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &RecordPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) GetRecord(ctx context.Context, id int64) (*Record, error) {
	stored, err := s.getStoredRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	record := stored.Record
	return &record, nil
}

func (s *Service) getStoredRecord(ctx context.Context, id int64) (*storedRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if id <= 0 {
		return nil, ErrRecordNotFound
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+recordColumns+`
		FROM invocation_archive_records WHERE id=$1`, id)
	record, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &storedRecord{Record: record}, nil
}

func (s *Service) ListAccessLogs(ctx context.Context, recordID int64, limit int) ([]AccessLog, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if recordID <= 0 {
		return nil, ErrRecordNotFound
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id,l.record_id,l.admin_id,COALESCE(u.email,''),l.reason,l.outcome,l.client_ip,l.user_agent,l.created_at
		FROM invocation_archive_access_logs l
		LEFT JOIN users u ON u.id=l.admin_id
		WHERE l.record_id=$1
		ORDER BY l.created_at DESC,l.id DESC
		LIMIT $2`, recordID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]AccessLog, 0)
	for rows.Next() {
		var item AccessLog
		var adminID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.RecordID, &adminID, &item.AdminName, &item.Reason, &item.Outcome, &item.ClientIP, &item.UserAgent, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.AdminID = nullableInt64Pointer(adminID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) RevealRecord(ctx context.Context, id, adminID int64, clientIP, userAgent string) (*Reveal, error) {
	if adminID <= 0 {
		return nil, infraerrors.Forbidden("invocation_archive_admin_required", "管理员身份无效")
	}
	stored, err := s.getStoredRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.activeConfig().DirectViewEnabled {
		_ = s.recordAccess(ctx, id, adminID, "", "direct_view_disabled", clientIP, userAgent)
		return nil, ErrDirectViewDisabled
	}
	if !time.Now().UTC().Before(stored.ExpiresAt) {
		_ = s.recordAccess(ctx, id, adminID, "", "expired", clientIP, userAgent)
		return nil, ErrPayloadExpired
	}
	requestStored, err := s.getStoredPayload(ctx, id, PayloadSlotRequest)
	if err != nil {
		return nil, err
	}
	responseStored, err := s.getStoredPayload(ctx, id, PayloadSlotResponse)
	if err != nil {
		return nil, err
	}
	request, err := s.revealStoredPayload(ctx, requestStored, PayloadSlotRequest)
	if err != nil {
		_ = s.recordAccess(ctx, id, adminID, "", "decrypt_failed", clientIP, userAgent)
		return nil, ErrPayloadUnavailable
	}
	response, err := s.revealStoredPayload(ctx, responseStored, PayloadSlotResponse)
	if err != nil {
		_ = s.recordAccess(ctx, id, adminID, "", "decrypt_failed", clientIP, userAgent)
		return nil, ErrPayloadUnavailable
	}
	if !request.Available && !response.Available {
		_ = s.recordAccess(ctx, id, adminID, "", "unavailable", clientIP, userAgent)
		return nil, ErrPayloadUnavailable
	}
	if err := s.recordAccess(ctx, id, adminID, "", "revealed", clientIP, userAgent); err != nil {
		return nil, fmt.Errorf("record invocation archive reveal before response: %w", err)
	}
	return &Reveal{RecordID: id, RevealedAt: time.Now().UTC(), Request: request, Response: response}, nil
}

func (s *Service) DeleteRecord(ctx context.Context, id int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("invocation archive database unavailable")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM invocation_archive_records WHERE id=$1`, id)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, ErrRecordNotFound
	}
	s.refreshStorageStats(ctx)
	return deleted, nil
}

func (s *Service) DeleteRecords(ctx context.Context, ids []int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("invocation archive database unavailable")
	}
	unique := uniquePositiveIDs(ids)
	if len(unique) == 0 || len(unique) > 100 {
		return 0, infraerrors.BadRequest("invocation_archive_record_ids_invalid", "归档记录 ID 列表无效")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM invocation_archive_records WHERE id=ANY($1)`, pq.Array(unique))
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if deleted > 0 {
		s.refreshStorageStats(ctx)
	}
	return deleted, nil
}

func (s *Service) ListSubjects(ctx context.Context, scope Scope, query string, limit int) ([]Subject, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invocation archive database unavailable")
	}
	if !validScope(scope) {
		return nil, infraerrors.BadRequest("invocation_archive_subject_scope_invalid", "归档范围类型无效")
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	needle := "%" + trimText(query, 128) + "%"
	subjectID, _ := strconv.ParseInt(strings.TrimSpace(query), 10, 64)
	var rows *sql.Rows
	var err error
	switch scope {
	case ScopeUser:
		rows, err = s.db.QueryContext(ctx, `
			SELECT id,COALESCE(NULLIF(username,''),email),email
			FROM users
			WHERE deleted_at IS NULL AND (id=$2 OR email ILIKE $1 OR username ILIKE $1)
			ORDER BY email ASC,id ASC LIMIT $3`, needle, subjectID, limit)
	case ScopeGroup:
		rows, err = s.db.QueryContext(ctx, `
			SELECT id,name,'' FROM groups
			WHERE deleted_at IS NULL AND (id=$2 OR name ILIKE $1)
			ORDER BY name ASC,id ASC LIMIT $3`, needle, subjectID, limit)
	case ScopeAPIKey:
		rows, err = s.db.QueryContext(ctx, `
			SELECT k.id,k.name,COALESCE(u.email,'')
			FROM api_keys k LEFT JOIN users u ON u.id=k.user_id
			WHERE k.deleted_at IS NULL AND (k.id=$2 OR k.name ILIKE $1 OR u.email ILIKE $1)
			ORDER BY k.name ASC,k.id ASC LIMIT $3`, needle, subjectID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Subject, 0)
	for rows.Next() {
		var item Subject
		if err := rows.Scan(&item.ID, &item.Label, &item.Secondary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) recordAccess(ctx context.Context, recordID, adminID int64, reason, outcome, clientIP, userAgent string) error {
	if s == nil || s.db == nil {
		return errors.New("invocation archive database unavailable")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO invocation_archive_access_logs(record_id,admin_id,reason,outcome,client_ip,user_agent)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		recordID, nullableID(adminID), trimText(reason, 256), trimText(outcome, 64), trimText(clientIP, 64), trimText(userAgent, 512))
	return err
}

func (s *Service) deleteExpired(ctx context.Context, now time.Time, retentionDays, limit int) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		WITH expired AS (
			SELECT id FROM invocation_archive_records
			WHERE expires_at <= $1
			   OR created_at <= ($1 - ($2 * INTERVAL '1 day'))
			ORDER BY expires_at ASC,id ASC
			LIMIT $3 FOR UPDATE SKIP LOCKED
		)
		DELETE FROM invocation_archive_records r
		USING expired WHERE r.id=expired.id`, now, retentionDays, limit)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	return int(deleted), err
}

func (s *Service) reconcileRetention(ctx context.Context, retentionDays int) error {
	if s == nil || s.db == nil {
		return errors.New("invocation archive database unavailable")
	}
	if retentionDays < 1 {
		return infraerrors.BadRequest("invocation_archive_retention_invalid", "归档保留天数无效")
	}
	maintenanceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), archiveMaintenanceTimeout)
	defer cancel()
	_, err := s.db.ExecContext(maintenanceCtx, `
		UPDATE invocation_archive_records
		SET expires_at = created_at + ($1 * INTERVAL '1 day')
		WHERE expires_at > created_at + ($1 * INTERVAL '1 day')`, retentionDays)
	return err
}

func (s *Service) deleteAll(ctx context.Context, limit int) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		WITH expired AS (
			SELECT id FROM invocation_archive_records
			ORDER BY id ASC
			LIMIT $1 FOR UPDATE SKIP LOCKED
		)
		DELETE FROM invocation_archive_records r
		USING expired WHERE r.id=expired.id`, limit)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	return int(deleted), err
}

func (s *Service) deleteExpiredAccessLogs(ctx context.Context, before time.Time, limit int) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		WITH expired AS (
			SELECT id FROM invocation_archive_access_logs
			WHERE created_at < $1
			ORDER BY created_at ASC,id ASC
			LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		DELETE FROM invocation_archive_access_logs a
		USING expired WHERE a.id=expired.id`, before, limit)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	return int(deleted), err
}

func recordWhere(filter RecordFilter) (string, []any, error) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 8)
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if query := trimText(filter.Query, 128); query != "" {
		add("(user_label ILIKE $%d OR api_key_name ILIKE $%d OR group_name ILIKE $%d OR model ILIKE $%d OR request_id ILIKE $%d OR client_request_id ILIKE $%d)", "%"+query+"%")
		// The query predicate needs the same parameter six times; PostgreSQL permits
		// a single placeholder to be referenced repeatedly, avoiding duplicate args.
		last := len(args)
		clauses[len(clauses)-1] = fmt.Sprintf("(user_label ILIKE $%d OR api_key_name ILIKE $%d OR group_name ILIKE $%d OR model ILIKE $%d OR request_id ILIKE $%d OR client_request_id ILIKE $%d)", last, last, last, last, last, last)
	}
	if filter.Mode != "" {
		if !validMode(filter.Mode) {
			return "", nil, infraerrors.BadRequest("invocation_archive_mode_invalid", "归档模式筛选无效")
		}
		add("mode=$%d", filter.Mode)
	}
	if filter.Outcome != "" {
		if !validOutcome(filter.Outcome) {
			return "", nil, infraerrors.BadRequest("invocation_archive_outcome_invalid", "归档结果筛选无效")
		}
		add("outcome=$%d", filter.Outcome)
	}
	if filter.UserID > 0 {
		add("user_id=$%d", filter.UserID)
	}
	if filter.GroupID > 0 {
		add("group_id=$%d", filter.GroupID)
	}
	if filter.APIKeyID > 0 {
		add("api_key_id=$%d", filter.APIKeyID)
	}
	if filter.From != nil {
		add("created_at >= $%d", filter.From.UTC())
	}
	if filter.To != nil {
		add("created_at <= $%d", filter.To.UTC())
	}
	return strings.Join(clauses, " AND "), args, nil
}

func validOutcome(value string) bool {
	switch value {
	case "completed", "client_error", "server_error", "websocket_error":
		return true
	default:
		return false
	}
}

func scanRecord(scanner interface{ Scan(...any) error }) (Record, error) {
	var record Record
	var userID, apiKeyID, groupID sql.NullInt64
	err := scanner.Scan(
		&record.ID, &record.CreatedAt, &record.CompletedAt, &record.ExpiresAt, &record.ConfigVersion, &record.Mode, &record.Transport, &record.WebSocketTurn,
		&userID, &record.UserLabel, &apiKeyID, &record.APIKeyName, &groupID, &record.GroupName,
		&record.RequestID, &record.ClientRequestID, &record.Method, &record.Path, &record.Model, &record.ClientIP, &record.UserAgent,
		&record.RequestContentType, &record.ResponseContentType, &record.HTTPStatus,
		&record.RequestTotalBytes, &record.RequestCapturedBytes, &record.RequestTruncated, &record.RequestStatus,
		&record.ResponseTotalBytes, &record.ResponseCapturedBytes, &record.ResponseTruncated, &record.ResponseStatus, &record.Outcome,
	)
	if err != nil {
		return Record{}, err
	}
	record.UserID = nullableInt64Pointer(userID)
	record.APIKeyID = nullableInt64Pointer(apiKeyID)
	record.GroupID = nullableInt64Pointer(groupID)
	return record, nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
