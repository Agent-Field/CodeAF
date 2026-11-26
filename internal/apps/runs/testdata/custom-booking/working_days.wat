(module
  (memory (export "memory") 1 1)
  (func (export "run") (param $from i64) (param $to i64) (param $holidays i64) (param $count i64) (result i64)
    (local $today i64) (local $weekday i64) (local $holiday i64) (local $total i64)
    local.get $from
    local.get $to
    i64.gt_s
    if
      i64.const -1
      return
    end
    local.get $from
    local.set $today
    block $finish
      loop $days
        local.get $today
        local.get $to
        i64.ge_s
        br_if $finish
        local.get $today
        i64.const 1
        i64.add
        local.tee $today
        i64.const 3
        i64.add
        i64.const 7
        i64.rem_s
        i64.const 7
        i64.add
        i64.const 7
        i64.rem_s
        local.set $weekday
        local.get $weekday
        i64.const 5
        i64.ge_s
        br_if $days
        i64.const 0
        local.set $holiday
        block $count_day
          loop $scan
            local.get $holiday
            local.get $count
            i64.ge_u
            br_if $count_day
            local.get $holidays
            local.get $holiday
            i64.const 8
            i64.mul
            i64.add
            i32.wrap_i64
            i64.load
            local.get $today
            i64.eq
            br_if $days
            local.get $holiday
            i64.const 1
            i64.add
            local.set $holiday
            br $scan
          end
        end
        local.get $total
        i64.const 1
        i64.add
        local.set $total
        br $days
      end
    end
    local.get $total
  )
)
