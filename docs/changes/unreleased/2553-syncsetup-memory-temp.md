---
kind: fixed
title: The syncsetup tests keep their temporary files on tmpfs when the machine has it
pr: 2553
surface: [build]
invalidates:
  - "#2548 said TestR9R23PersonalLearnerRefreshesColdRouterGeneration failed because the restarted relay's fake clock never ticked, and made the test advance it; that was wrong and is taken back. The test failed when 188 FULL-synchronous fsyncs on a busy disk added up past its 30-second timeout, and the syncsetup package now keeps its temporary files on /dev/shm when that is tmpfs with at least 1 GiB free."
---
Each run says `syncsetup: temporary files on tmpfs at …` on stderr when it moves. A
log without that line ran on the ordinary disk. The product cost this exposed, a
Go relay's cold activation outliving its own client timeout on a slow disk, is
#2552.
