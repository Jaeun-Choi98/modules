package utils

import (
	"fmt"
	"math"
	"reflect"
	"time"

	"golang.org/x/exp/constraints"
)

var (
	LocalKorea = NewKoreaLocation()
)

func NewKoreaLocation() *time.Location {
	local, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return nil
	}
	return local
}

func ValToIdx[T constraints.Integer](v T) []int {
	var ret []int
	i := 0
	for v > 0 {
		if (v & (1 << i)) != 0 {
			ret = append(ret, i+1)
			v = v ^ (1 << i)
		}
		i++
	}
	return ret
}

func IdxToVal[T constraints.Integer](idxs []int) T {
	var result T = 0

	for _, idx := range idxs {
		if idx > 0 {
			result |= T(1 << (idx - 1))
		}
	}

	return result
}

/**
 * 깊은 복사
 * @param src interface{} - 복사할 구조체 (포인터)
 * @param dst는 interface{} - 목적지 구조체 (포인터)
 */
func DeepCopy(src, dst interface{}) {
	s := reflect.ValueOf(src)
	d := reflect.ValueOf(dst)

	// 포인터가 아니면 수정 불가
	if s.Kind() != reflect.Ptr || d.Kind() != reflect.Ptr {
		return
	}

	DeepCopyValue(s.Elem(), d.Elem())
}

func DeepCopyValue(src, dst reflect.Value) {
	switch src.Kind() {
	case reflect.Pointer:
		if !src.IsNil() {
			dst.Set(reflect.New(src.Elem().Type()))
			DeepCopyValue(src.Elem(), dst.Elem())
		}
	case reflect.Struct:
		for i := 0; i < src.NumField(); i++ {
			DeepCopyValue(src.Field(i), dst.Field(i))
		}
	case reflect.Slice:
		if !src.IsNil() {
			dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Cap()))
			for i := 0; i < src.Len(); i++ {
				DeepCopyValue(src.Index(i), dst.Index(i))
			}
		}
	case reflect.Map:
		if !src.IsNil() {
			dst.Set(reflect.MakeMap(src.Type()))
			keys := src.MapKeys()
			for _, key := range keys {
				srcValue := src.MapIndex(key)
				var dstValue reflect.Value
				if srcValue.Kind() == reflect.Pointer {
					if !srcValue.IsNil() {
						dstValue = reflect.New(srcValue.Elem().Type())
						DeepCopyValue(srcValue.Elem(), dstValue.Elem())
					} else {
						dstValue = reflect.Zero(srcValue.Type())
					}
				} else {
					dstValue = reflect.New(srcValue.Type()).Elem()
					DeepCopyValue(srcValue, dstValue)
				}
				dst.SetMapIndex(key, dstValue)
			}
		}
	default:
		dst.Set(src)
	}
}

/**
 * 정수값이나 실수값을 받아 Number(p,s), Decimal(p,s)에 맞게 파싱하는 함수.
 */
type Number interface {
	constraints.Integer | constraints.Float
}

func ConvertDecimal[T Number](t T, p, s int) T {
	val := float64(t)

	// 스케일 팩터 계산 (10^s)
	scaleFactor := math.Pow(10, float64(s))

	// 소수점 s자리로 반올림
	rounded := math.Round(val*scaleFactor) / scaleFactor

	// 전체 자릿수 p를 초과하는지 확인
	maxValue := math.Pow(10, float64(p-s)) - 1/scaleFactor
	if math.Abs(rounded) > maxValue {
		// 범위를 초과하면 최대값으로 클램핑하거나 패닉 처리
		// 여기서는 클램핑으로 처리
		if rounded > 0 {
			rounded = maxValue
		} else {
			rounded = -maxValue
		}
	}

	return T(rounded)
}

/**
 * 구조체 필드를 순차적으로 수정하는 표준 함수
 * @param s interface{} - 수정할 구조체 (포인터)
 * @param startIdx int - 시작 필드 인덱스
 * @param endIdx int - 종료 필드 인덱스 (-1이면 마지막까지)
 * @param params ...interface{} - 순차적으로 적용할 값들
 */
func ModifyStructByIndex(s interface{}, startIdx int, endIdx int, params []interface{}) error {
	v := reflect.ValueOf(s)

	// 포인터가 아니면 수정 불가
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("utils err: modifying struct fields line 17: not a pointer")
	}

	// 실제 값 가져오기
	v = v.Elem()
	t := v.Type()

	totalFields := v.NumField()

	// 인덱스 검증 및 조정
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx < 0 || endIdx >= totalFields {
		endIdx = totalFields - 1
	}
	if startIdx > endIdx {
		return fmt.Errorf("utils err: modifying struct fields line 33: start index %d > end index %d", startIdx, endIdx)
	}

	paramIdx := 0

	for i := startIdx; i <= endIdx && i < totalFields; i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// 수정 가능한지 확인
		if !field.CanSet() {
			continue
		}

		// 파라미터가 있으면 사용, 없으면 스킵
		if paramIdx >= len(params) {
			break
		}
		newValue := params[paramIdx]

		// 타입이 정확히 일치하는지 확인 후 설정
		if err := setFieldValue(field, newValue, fieldType.Name); err != nil {
			return err
		}
		paramIdx++
	}
	return nil
}

/**
 * 맵을 사용해서 구조체 필드를 선택적으로 수정
 * @param s interface{} - 수정할 구조체 (포인터)
 * @param fieldValues map[string]interface{} - 필드명:값 맵
 */
func ModifyStructByMap(s interface{}, fieldValues map[string]interface{}) error {
	v := reflect.ValueOf(s)

	// 포인터가 아니면 수정 불가
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("not a pointer")
	}

	// 실제 값 가져오기
	v = v.Elem()

	// 각 필드별로 값 설정
	for fieldName, newValue := range fieldValues {
		field := v.FieldByName(fieldName)

		// 필드가 존재하지 않으면 에러
		if !field.IsValid() {
			return fmt.Errorf("field '%s' not found", fieldName)
		}

		// 수정 가능한지 확인
		if !field.CanSet() {
			return fmt.Errorf("field '%s' cannot be set", fieldName)
		}

		// 값 설정
		if err := setFieldValue(field, newValue, fieldName); err != nil {
			return err
		}
	}

	return nil
}

func setFieldValue(field reflect.Value, newValue interface{}, fieldName string) error {
	if newValue == nil {
		return fmt.Errorf("field '%s' cannot set nil value", fieldName)
	}

	newVal := reflect.ValueOf(newValue)
	fieldType := field.Type()

	// 타입이 다르면 변환 시도
	if newVal.Type() != fieldType {
		if newVal.Type().ConvertibleTo(fieldType) {
			newVal = newVal.Convert(fieldType)
		} else {
			return fmt.Errorf("field '%s' type mismatch: expected %s, got %s",
				fieldName, fieldType, newVal.Type())
		}
	}

	field.Set(newVal)
	return nil
}
