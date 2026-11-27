package main

import "github.com/banjo/nota/internal/attachments"

// Attachments lists the files in the vault's attachments folder with the
// notes that use each one.
func (s *Vault) Attachments() (*attachments.List, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return attachments.Load(s.v, s.ix)
}
