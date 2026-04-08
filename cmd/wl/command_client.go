package main

import (
	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/sdk"
)

// commandClient abstracts sdk.Client so CLI handlers can be tested without
// constructing real SDK/database stacks.
type commandClient interface {
	Browse(filter commons.BrowseFilter) (*sdk.BrowseResult, error)
	Detail(wantedID string) (*sdk.DetailResult, error)
	Claim(wantedID string) (*sdk.MutationResult, error)
	Unclaim(wantedID string) (*sdk.MutationResult, error)
	Done(wantedID, evidence string) (*sdk.MutationResult, error)
	Accept(wantedID string, input sdk.AcceptInput) (*sdk.MutationResult, error)
	AcceptUpstream(wantedID, submitterHandle string, input sdk.AcceptInput) (*sdk.MutationResult, error)
	Reject(wantedID, reason string) (*sdk.MutationResult, error)
	RejectUpstream(wantedID, submitterHandle string) error
	Close(wantedID string) (*sdk.MutationResult, error)
	CloseUpstream(wantedID, submitterHandle string) (*sdk.MutationResult, error)
	Delete(wantedID string) (*sdk.MutationResult, error)
	Post(input sdk.PostInput) (*sdk.MutationResult, error)
	Update(wantedID string, fields *commons.WantedUpdate) (*sdk.MutationResult, error)
}

var newCommandClient = func(cfg *federation.Config, noPush bool) (commandClient, error) {
	return newSDKClient(cfg, noPush)
}
