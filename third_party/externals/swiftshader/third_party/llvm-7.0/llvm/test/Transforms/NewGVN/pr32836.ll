; NOTE: Assertions have been autogenerated by utils/update_test_checks.py
; RUN: opt -S -newgvn %s | FileCheck %s

%struct.anon = type { i32 }
@b = external global %struct.anon
define void @tinkywinky(i1 %patatino) {
; CHECK-LABEL: @tinkywinky(
; CHECK-NEXT:    store i32 8, i32* null
; CHECK-NEXT:    br i1 [[PATATINO:%.*]], label [[IF_END:%.*]], label [[IF_THEN:%.*]]
; CHECK:       if.then:
; CHECK-NEXT:    br label [[L:%.*]]
; CHECK:       L:
; CHECK-NEXT:    br label [[IF_END]]
; CHECK:       if.end:
; CHECK-NEXT:    [[TMP1:%.*]] = load i32, i32* null
; CHECK-NEXT:    [[BF_LOAD1:%.*]] = load i32, i32* getelementptr inbounds (%struct.anon, %struct.anon* @b, i64 0, i32 0)
; CHECK-NEXT:    [[BF_VALUE:%.*]] = and i32 [[TMP1]], 536870911
; CHECK-NEXT:    [[BF_CLEAR:%.*]] = and i32 [[BF_LOAD1]], -536870912
; CHECK-NEXT:    [[BF_SET:%.*]] = or i32 [[BF_CLEAR]], [[BF_VALUE]]
; CHECK-NEXT:    store i32 [[BF_SET]], i32* getelementptr inbounds (%struct.anon, %struct.anon* @b, i64 0, i32 0)
; CHECK-NEXT:    br label [[LOR_END:%.*]]
; CHECK:       lor.end:
; CHECK-NEXT:    br label [[L]]
;
  store i32 8, i32* null
  br i1 %patatino, label %if.end, label %if.then
if.then:
  store i32 8, i32* null
  br label %L
L:
  br label %if.end
if.end:
  %tmp1 = load i32, i32* null
  %bf.load1 = load i32, i32* getelementptr (%struct.anon, %struct.anon* @b, i64 0, i32 0)
  %bf.value = and i32 %tmp1, 536870911
  %bf.clear = and i32 %bf.load1, -536870912
  %bf.set = or i32 %bf.clear, %bf.value
  store i32 %bf.set, i32* getelementptr (%struct.anon, %struct.anon* @b, i64 0, i32 0)
  br label %lor.end
lor.end:
  %bf.load4 = load i32, i32* getelementptr (%struct.anon, %struct.anon* @b, i64 0, i32 0)
  %tmp4 = and i32 %bf.load4, 536870911
  %or = or i32 0, %tmp4
  br label %L
}
