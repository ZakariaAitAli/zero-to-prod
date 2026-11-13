# Interrupted instrumentation trial

Baseline and 60 jobs/s client/job results passed, but review found that old
Prometheus target samples briefly survived container replacement. Summing them
could overcount replica CPU and pool measurements. Shared-database backlog
also needs max across API replicas, not sum. The trial was interrupted before
using those measurements for conclusions. The harness now restricts application
queries to actual current container endpoints and checks discovery before load.
Run 2 is the recorded matrix. Retained run 1 values are diagnostic only.
