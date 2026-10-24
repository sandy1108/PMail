package response

import (
	"encoding/json"
	"github.com/Jinnrry/pmail/models"
)

type EmailResponseData struct {
	models.Email      `xorm:"extends"`
	IMAPDeleted       bool     `xorm:"-" json:"-"`
	DeliveryAddresses []string `xorm:"-" json:"delivery_addresses"`
	IsRead            int8     `json:"is_read"`
	SerialNumber      int      `json:"serial_number"`
	UeId              int      `json:"ue_id"`
}

type UserEmailUIDData struct {
	models.UserEmail `xorm:"extends"`
	SerialNumber     int `json:"serial_number"`
}

// 显式合并详情字段，避免内嵌Email的MarshalJSON吞掉用户独立状态和投递来源。
func (d *EmailResponseData) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(&d.Email)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	extras := map[string]any{"is_read": d.IsRead, "serial_number": d.SerialNumber, "ue_id": d.UeId, "delivery_addresses": d.DeliveryAddresses}
	for key, value := range extras {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = encoded
	}
	return json.Marshal(fields)
}
