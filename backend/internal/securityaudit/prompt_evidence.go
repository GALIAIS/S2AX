package securityaudit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultEvidenceRetention = 30 * 24 * time.Hour
	evidenceMigrationBatch   = 100
)

type EvidenceStatus string

const (
	EvidenceNotStored        EvidenceStatus = "not_stored"
	EvidenceEncrypted        EvidenceStatus = "encrypted"
	EvidenceExpired          EvidenceStatus = "expired"
	EvidenceEncryptionFailed EvidenceStatus = "encryption_failed"
	EvidenceLegacyPlaintext  EvidenceStatus = "legacy_plaintext"
)

var (
	ErrEvidenceUnavailable   = errors.New("prompt audit evidence unavailable")
	ErrEvidenceExpired       = errors.New("prompt audit evidence expired")
	ErrEvidenceReasonInvalid = errors.New("prompt audit evidence reveal reason invalid")
)

type EvidenceReveal struct {
	EventID    int64     `json:"event_id"`
	FullPrompt string    `json:"full_prompt"`
	RevealedAt time.Time `json:"revealed_at"`
}

type legacyEvidence struct {
	EventID    int64
	FullPrompt string
}

func protectPromptEvidence(snapshot PromptSnapshot, result *NormalizedResult, encryptor SecretEncryptor, now time.Time) (PromptSnapshot, error) {
	plain := snapshot.FullPrompt
	snapshot.FullPrompt = ""
	snapshot.ScanText = ""
	snapshot.EvidenceCiphertext = ""
	snapshot.EvidenceExpiresAt = nil
	snapshot.EvidenceStatus = EvidenceNotStored
	if result == nil || result.Decision == EventPass || plain == "" {
		return snapshot, nil
	}
	if encryptor == nil {
		snapshot.EvidenceStatus = EvidenceEncryptionFailed
		return snapshot, errors.New("prompt audit evidence encryptor unavailable")
	}
	ciphertext, err := encryptor.Encrypt(plain)
	if err != nil || strings.TrimSpace(ciphertext) == "" {
		snapshot.EvidenceStatus = EvidenceEncryptionFailed
		if err == nil {
			err = errors.New("prompt audit evidence encryption returned empty ciphertext")
		}
		return snapshot, err
	}
	expiresAt := now.UTC().Add(DefaultEvidenceRetention)
	snapshot.EvidenceCiphertext = ciphertext
	snapshot.EvidenceStatus = EvidenceEncrypted
	snapshot.EvidenceExpiresAt = &expiresAt
	return snapshot, nil
}

func (s *PromptService) startEvidenceMaintenance(ctx context.Context) {
	if s == nil || s.repo == nil || s.repo.db == nil || s.config == nil {
		return
	}
	s.maintenanceWG.Add(1)
	go func() {
		defer s.maintenanceWG.Done()
		s.runEvidenceMaintenance(ctx)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runEvidenceMaintenance(ctx)
			}
		}
	}()
}

func (s *PromptService) runEvidenceMaintenance(ctx context.Context) {
	for {
		items, err := s.repo.ListLegacyPlaintextEvidence(ctx, evidenceMigrationBatch)
		if err != nil {
			LogWarn(EventProcessFailed, map[string]any{"status": "evidence_migration_failed", "error_code": "evidence_migration_read_failed"})
			break
		}
		if len(items) == 0 {
			break
		}
		for index := range items {
			item := &items[index]
			ciphertext, encryptErr := s.config.Encrypt(item.FullPrompt)
			item.FullPrompt = ""
			status := EvidenceEncrypted
			var expiresAt *time.Time
			if encryptErr != nil || strings.TrimSpace(ciphertext) == "" {
				ciphertext = ""
				status = EvidenceEncryptionFailed
			} else {
				value := s.clock.Now().UTC().Add(DefaultEvidenceRetention)
				expiresAt = &value
			}
			if err := s.repo.ReplaceLegacyPlaintextEvidence(ctx, item.EventID, ciphertext, status, expiresAt); err != nil {
				LogWarn(EventProcessFailed, map[string]any{"event_id": item.EventID, "status": "evidence_migration_failed", "error_code": "evidence_migration_write_failed"})
				return
			}
		}
		if len(items) < evidenceMigrationBatch {
			break
		}
	}
	for {
		purged, err := s.repo.PurgeExpiredEvidence(ctx, s.clock.Now().UTC(), evidenceMigrationBatch)
		if err != nil {
			LogWarn(EventProcessFailed, map[string]any{"status": "evidence_expiry_failed", "error_code": "evidence_expiry_write_failed"})
			break
		}
		if purged < evidenceMigrationBatch {
			break
		}
	}
}

func (s *PromptService) RevealEventEvidence(ctx context.Context, eventID, adminID int64, reason string) (*EvidenceReveal, error) {
	reason = strings.TrimSpace(reason)
	reasonLength := len([]rune(reason))
	if eventID <= 0 || adminID <= 0 || reasonLength < 3 || reasonLength > 256 {
		return nil, ErrEvidenceReasonInvalid
	}
	ciphertext, status, expiresAt, err := s.repo.GetEventEvidence(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if status == EvidenceExpired || (expiresAt != nil && !s.clock.Now().UTC().Before(*expiresAt)) {
		return nil, errors.Join(
			ErrEvidenceExpired,
			s.recordPromptEvidenceAccess(ctx, eventID, adminID, reason, "expired"),
		)
	}
	if status != EvidenceEncrypted || strings.TrimSpace(ciphertext) == "" {
		return nil, errors.Join(
			ErrEvidenceUnavailable,
			s.recordPromptEvidenceAccess(ctx, eventID, adminID, reason, "unavailable"),
		)
	}
	plain, err := s.config.Decrypt(ciphertext)
	if err != nil {
		return nil, errors.Join(
			ErrEvidenceUnavailable,
			s.recordPromptEvidenceAccess(ctx, eventID, adminID, reason, "decrypt_failed"),
		)
	}
	if err := s.recordPromptEvidenceAccess(ctx, eventID, adminID, reason, "revealed"); err != nil {
		return nil, fmt.Errorf("record prompt evidence access before reveal: %w", err)
	}
	return &EvidenceReveal{EventID: eventID, FullPrompt: plain, RevealedAt: s.clock.Now().UTC()}, nil
}

func (s *PromptService) recordPromptEvidenceAccess(
	ctx context.Context,
	eventID, adminID int64,
	reason, outcome string,
) error {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return s.repo.RecordEvidenceAccess(auditCtx, eventID, adminID, reason, outcome)
}
