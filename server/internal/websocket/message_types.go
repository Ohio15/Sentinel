package websocket

// Extended message types for inventory and mobile support

// Inventory collection message types
const (
	MsgTypeInventoryFull     = "inventory_full"
	MsgTypeInventoryDelta    = "inventory_delta"
	MsgTypeSecurityPosture   = "security_posture"
	MsgTypeUserAccess        = "user_access"
	MsgTypeHardwareInventory = "hardware_inventory"
	MsgTypeRequestInventory  = "request_inventory"
	MsgTypeInventoryConfig   = "inventory_config"
)

// Mobile device message types
const (
	MsgTypeMobileMetrics    = "mobile_metrics"
	MsgTypeMobileHeartbeat  = "mobile_heartbeat"
	MsgTypeMobileLocation   = "mobile_location"
	MsgTypeMobileApps       = "mobile_apps"
	MsgTypeMobileBattery    = "mobile_battery"
	MsgTypeMobileSecurity   = "mobile_security"
	MsgTypeMobileLock       = "mobile_lock"
	MsgTypeMobileWipe       = "mobile_wipe"
	MsgTypeMobileMessage    = "mobile_message"
	MsgTypeMobileCompliance = "mobile_compliance"
	MsgTypePushRegister     = "push_register"
	MsgTypePushCommand      = "push_command"
)
