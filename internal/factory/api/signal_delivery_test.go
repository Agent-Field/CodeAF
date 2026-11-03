package api

import (
 "encoding/json"
 "reflect"
 "testing"
)

func TestSignalDeliveryAndSemanticIDsRoundTripIndependently(t *testing.T) {
 automatic:=true
 want:=WorkerSignal{Delivery:"lease/signal",ID:"question-or-message",Verb:"answer",Words:"chosen",Yes:true,Budget:8,Automatic:&automatic}
 raw,err:=json.Marshal(want);if err!=nil{t.Fatal(err)}
 var got WorkerSignal
 if err:=json.Unmarshal(raw,&got);err!=nil{t.Fatal(err)}
 if !reflect.DeepEqual(want,got){t.Fatalf("signal wire lost a field: want %+v got %+v",want,got)}
}
