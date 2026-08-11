package mbim

const (
	CIDDeviceCaps                 = 0x00000001
	CIDRadioState                 = 0x00000003
	CIDSubscriberReadyStatus      = 0x00000002
	CIDPIN                        = 0x00000004
	CIDPINList                    = 0x00000005
	CIDHomeProvider               = 0x00000006
	CIDPreferredProviders         = 0x00000007
	CIDVisibleProviders           = 0x00000008
	CIDRegisterState              = 0x00000009
	CIDPacketService              = 0x0000000A
	CIDSignalState                = 0x0000000B
	CIDConnect                    = 0x0000000C
	CIDProvisionedContexts        = 0x0000000D
	CIDServiceActivation          = 0x0000000E
	CIDIPConfiguration            = 0x0000000F
	CIDDeviceServices             = 0x00000010
	CIDDeviceServiceSubscribeList = 0x00000013
	CIDPacketStatistics           = 0x00000014
	CIDNetworkIdleHint            = 0x00000015
	CIDEmergencyMode              = 0x00000016
	CIDIPPacketFilters            = 0x00000017
	CIDMulticarrierProviders      = 0x00000018

	CIDSMSConfiguration      = 0x00000001
	CIDSMSRead               = 0x00000002
	CIDSMSSend               = 0x00000003
	CIDSMSDelete             = 0x00000004
	CIDSMSMessageStoreStatus = 0x00000005

	CIDUSSD = 0x00000001

	CIDPhonebookConfiguration = 0x00000001
	CIDPhonebookRead          = 0x00000002
	CIDPhonebookDelete        = 0x00000003
	CIDPhonebookWrite         = 0x00000004

	CIDAuthAKA  = 0x00000001
	CIDAuthAKAP = 0x00000002
	CIDAuthSIM  = 0x00000003

	CIDDSSConnect              = 0x00000001
	CIDMSFirmwareIDGet         = 0x00000001
	CIDMSHostShutdownNotify    = 0x00000001
	CIDMSSARConfig             = 0x00000001
	CIDMSSARTransmissionStatus = 0x00000002
	CIDMSVoiceExtensionsNITZ   = 0x0000000A

	CIDSTKPAC              = 0x00000001
	CIDSTKTerminalResponse = 0x00000002
	CIDSTKEnvelope         = 0x00000003

	CIDUICCATR                = 0x00000001
	CIDUICCOpenChannel        = 0x00000002
	CIDUICCCloseChannel       = 0x00000003
	CIDUICCAPDU               = 0x00000004
	CIDUICCTerminalCapability = 0x00000005
	CIDUICCReset              = 0x00000006
	CIDUICCApplicationList    = 0x00000007
	CIDUICCFileStatus         = 0x00000008
	CIDUICCReadBinary         = 0x00000009
	CIDUICCReadRecord         = 0x0000000A

	CIDProxyControlConfiguration = 0x00000001
	CIDProxyControlVersion       = 0x00000002
	CIDDeviceSlotMappings        = 0x00000007
	CIDVersion                   = 0x0000000F
	CIDMSProvisionedContexts     = 0x00000001
	CIDMSLTEAttachConfiguration  = 0x00000003
	CIDMSLTEAttachInfo           = 0x00000004
	CIDMSSystemCapabilities      = 0x00000005
	CIDMSDeviceCapsV2            = 0x00000006
	CIDMSSlotInfoStatus          = 0x00000008
	CIDMSPCO                     = 0x00000009
	CIDMSDeviceReset             = 0x0000000A
	CIDMSBaseStationsInfo        = 0x0000000B
	CIDMSLocationInfoStatus      = 0x0000000C
	CIDMSModemConfiguration      = 0x00000010
	CIDMSRegistrationParameters  = 0x00000011
	CIDMSNetworkParameters       = 0x00000012
	CIDMSWakeReason              = 0x00000013
	CIDMSUEPolicy                = 0x00000014
)
