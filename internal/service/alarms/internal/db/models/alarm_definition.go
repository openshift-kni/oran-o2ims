package models

import "github.com/google/uuid"

type AlarmDefinition struct {
	AlarmDefinitionID     uuid.UUID `db:"alarm_definition_id"`
	AlarmName             string    `db:"alarm_name"`
	AlarmLastChange       string    `db:"alarm_last_change"`
	AlarmChangeType       string    `db:"alarm_change_type"`
	AlarmDescription      string    `db:"alarm_description"`
	ProposedRepairActions string    `db:"proposed_repair_actions"`
	ClearingType          string    `db:"clearing_type"`
	ManagementInterfaceID []string  `db:"management_interface_id"`
	PkNotificationField   []string  `db:"pk_notification_field"`
	AlarmAdditionalFields string    `db:"alarm_additional_fields"`
	AlarmDictionaryID     uuid.UUID `db:"alarm_dictionary_id"`
	ResourceTypeID        uuid.UUID `db:"resource_type_id"`
	ProbableCauseID       uuid.UUID `db:"probable_cause_id"`
	CreatedAt             string    `db:"created_at"`
	UpdatedAt             string    `db:"updated_at"`
}
