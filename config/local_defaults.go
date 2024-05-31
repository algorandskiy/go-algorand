// Copyright (C) 2019-2024 Algorand, Inc.
// This file is part of go-algorand
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

// This file was auto generated by ./config/defaultsGenerator/defaultsGenerator.go, and SHOULD NOT BE MODIFIED in any way
// If you want to make changes to this file, make the corresponding changes to Local in localTemplate.go and run "go generate".

package config

var defaultLocal = Local{
	Version:                                    34,
	AccountUpdatesStatsInterval:                5000000000,
	AccountsRebuildSynchronousMode:             1,
	AgreementIncomingBundlesQueueLength:        15,
	AgreementIncomingProposalsQueueLength:      50,
	AgreementIncomingVotesQueueLength:          20000,
	AnnounceParticipationKey:                   true,
	Archival:                                   false,
	BaseLoggerDebugLevel:                       4,
	BlockDBDir:                                 "",
	BlockServiceCustomFallbackEndpoints:        "",
	BlockServiceMemCap:                         500000000,
	BroadcastConnectionsLimit:                  -1,
	CadaverDirectory:                           "",
	CadaverSizeTarget:                          0,
	CatchpointDir:                              "",
	CatchpointFileHistoryLength:                365,
	CatchpointInterval:                         10000,
	CatchpointTracking:                         0,
	CatchupBlockDownloadRetryAttempts:          1000,
	CatchupBlockValidateMode:                   0,
	CatchupFailurePeerRefreshRate:              10,
	CatchupGossipBlockFetchTimeoutSec:          4,
	CatchupHTTPBlockFetchTimeoutSec:            4,
	CatchupLedgerDownloadRetryAttempts:         50,
	CatchupParallelBlocks:                      16,
	ColdDataDir:                                "",
	ConnectionsRateLimitingCount:               60,
	ConnectionsRateLimitingWindowSeconds:       1,
	CrashDBDir:                                 "",
	DNSBootstrapID:                             "<network>.algorand.network?backup=<network>.algorand.net&dedup=<name>.algorand-<network>.(network|net)",
	DNSSecurityFlags:                           9,
	DeadlockDetection:                          0,
	DeadlockDetectionThreshold:                 30,
	DisableAPIAuth:                             false,
	DisableLedgerLRUCache:                      false,
	DisableLocalhostConnectionRateLimit:        true,
	DisableNetworking:                          false,
	DisableOutgoingConnectionThrottling:        false,
	EnableAccountUpdatesStats:                  false,
	EnableAgreementReporting:                   false,
	EnableAgreementTimeMetrics:                 false,
	EnableAssembleStats:                        false,
	EnableBlockService:                         false,
	EnableDHTProviders:                         false,
	EnableDeveloperAPI:                         false,
	EnableExperimentalAPI:                      false,
	EnableFollowMode:                           false,
	EnableGossipBlockService:                   true,
	EnableGossipService:                        true,
	EnableIncomingMessageFilter:                false,
	EnableLedgerService:                        false,
	EnableMetricReporting:                      false,
	EnableOutgoingNetworkMessageFiltering:      true,
	EnableP2P:                                  false,
	EnableP2PHybridMode:                        false,
	EnablePingHandler:                          true,
	EnableProcessBlockStats:                    false,
	EnableProfiler:                             false,
	EnableRequestLogger:                        false,
	EnableRuntimeMetrics:                       false,
	EnableTopAccountsReporting:                 false,
	EnableTxBacklogAppRateLimiting:             true,
	EnableTxBacklogRateLimiting:                true,
	EnableTxnEvalTracer:                        false,
	EnableUsageLog:                             false,
	EnableVerbosedTransactionSyncLogging:       false,
	EndpointAddress:                            "127.0.0.1:0",
	FallbackDNSResolverAddress:                 "",
	ForceFetchTransactions:                     false,
	ForceRelayMessages:                         false,
	GossipFanout:                               4,
	HeartbeatUpdateInterval:                    600,
	HotDataDir:                                 "",
	IncomingConnectionsLimit:                   2400,
	IncomingMessageFilterBucketCount:           5,
	IncomingMessageFilterBucketSize:            512,
	LedgerSynchronousMode:                      2,
	LogArchiveDir:                              "",
	LogArchiveMaxAge:                           "",
	LogArchiveName:                             "node.archive.log",
	LogFileDir:                                 "",
	LogSizeLimit:                               1073741824,
	MaxAPIBoxPerApplication:                    100000,
	MaxAPIResourcesPerAccount:                  100000,
	MaxAcctLookback:                            4,
	MaxBlockHistoryLookback:                    0,
	MaxCatchpointDownloadDuration:              43200000000000,
	MaxConnectionsPerIP:                        15,
	MinCatchpointFileDownloadBytesPerSecond:    20480,
	NetAddress:                                 "",
	NetworkMessageTraceServer:                  "",
	NetworkProtocolVersion:                     "",
	NodeExporterListenAddress:                  ":9100",
	NodeExporterPath:                           "./node_exporter",
	OptimizeAccountsDatabaseOnStartup:          false,
	OutgoingMessageFilterBucketCount:           3,
	OutgoingMessageFilterBucketSize:            128,
	P2PListenAddress:                           "",
	P2PPersistPeerID:                           false,
	P2PPrivateKeyLocation:                      "",
	ParticipationKeysRefreshInterval:           60000000000,
	PeerConnectionsUpdateInterval:              3600,
	PeerPingPeriodSeconds:                      0,
	PriorityPeers:                              map[string]bool{},
	ProposalAssemblyTime:                       500000000,
	PublicAddress:                              "",
	ReconnectTime:                              60000000000,
	ReservedFDs:                                256,
	RestConnectionsHardLimit:                   2048,
	RestConnectionsSoftLimit:                   1024,
	RestReadTimeoutSeconds:                     15,
	RestWriteTimeoutSeconds:                    120,
	RunHosted:                                  false,
	StateproofDir:                              "",
	StorageEngine:                              "sqlite",
	SuggestedFeeBlockHistory:                   3,
	SuggestedFeeSlidingWindowSize:              50,
	TLSCertFile:                                "",
	TLSKeyFile:                                 "",
	TelemetryToLog:                             true,
	TrackerDBDir:                               "",
	TransactionSyncDataExchangeRate:            0,
	TransactionSyncSignificantMessageThreshold: 0,
	TxBacklogAppTxPerSecondRate:                100,
	TxBacklogAppTxRateLimiterMaxSize:           1048576,
	TxBacklogRateLimitingCongestionPct:         50,
	TxBacklogReservedCapacityPerPeer:           20,
	TxBacklogServiceRateWindowSeconds:          10,
	TxBacklogSize:                              26000,
	TxIncomingFilterMaxSize:                    500000,
	TxIncomingFilteringFlags:                   1,
	TxPoolExponentialIncreaseFactor:            2,
	TxPoolSize:                                 75000,
	TxSyncIntervalSeconds:                      60,
	TxSyncServeResponseSize:                    1000000,
	TxSyncTimeoutSeconds:                       30,
	UseXForwardedForAddressField:               "",
	VerifiedTranscationsCacheSize:              150000,
}
