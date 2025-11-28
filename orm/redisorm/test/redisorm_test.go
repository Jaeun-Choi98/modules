package test

import (
	"testing"
	"time"

	"github.com/Jaeun-Choi98/modules/orm/redisorm"
	"github.com/stretchr/testify/assert"
)

var addr = ``
var psssword = ``

// Device 모델
type Device struct {
	Id           int64             `json:"id"`
	SerialNumber string            `json:"serial_number"`
	Model        string            `json:"model"`
	Status       string            `json:"status"`
	Power        float64           `json:"power"`
	Specs        map[string]string `json:"specs"` // 복잡한 구조도 JSON으로 쉽게!
	Tags         []string          `json:"tags"`
}

func (d *Device) GetId() int64      { return d.Id }
func (d *Device) SetId(id int64)    { d.Id = id }
func (d *Device) TableName() string { return "devices" }
func (d *Device) GetIndexFields() map[string]interface{} {
	return map[string]interface{}{
		"serial_number": d.SerialNumber,
		"status":        d.Status,
	}
}
func (d *Device) GetTTL() time.Duration {
	return 0
}

func TestRedisORMCreateAndFind(t *testing.T) {
	redisClient := redisorm.NewRedisClient(addr, psssword, 0, 2, 5)
	dvcRepository := redisorm.NewRepository(redisClient, &Device{})
	dvc1 := &Device{
		SerialNumber: "1asd32d213",
		Model:        "test model",
		Status:       "test status",
		Power:        324.512,
		Specs:        map[string]string{"spec1": "sdfds", "spec2": "sdfdsdsf"},
		Tags:         []string{"tag1", "tag2"},
	}
	err := dvcRepository.Create(dvc1)
	assert.NoError(t, err, "")
	t.Log(dvcRepository.FindAll())
	val, _ := dvcRepository.FindByIndex("serial_number", dvc1.SerialNumber)
	t.Log(val)
}

func TestRedisORMUpdate(t *testing.T) {
	redisClient := redisorm.NewRedisClient(addr, psssword, 0, 2, 5)
	dvcRepository := redisorm.NewRepository(redisClient, &Device{})
	dvc1, _ := dvcRepository.FindByIndex("serial_number", "1asd32d213")
	t.Logf("%v", dvc1)
	dvc1.SerialNumber = "update serial number"
	dvc1.Specs = map[string]string{"spec1": "update spec1", "spec2": "update spec2"}
	err := dvcRepository.Update(dvc1)
	assert.NoError(t, err)
}

func TestRedisORMDelete(t *testing.T) {
	redisClient := redisorm.NewRedisClient(addr, psssword, 0, 2, 5)
	dvcRepository := redisorm.NewRepository(redisClient, &Device{})
	dvc1, _ := dvcRepository.FindByIndex("serial_number", "update serial number")
	err := dvcRepository.Delete(dvc1.Id)
	assert.NoError(t, err)
}
