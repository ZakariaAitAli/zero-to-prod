REVOKE UPDATE (
    state,
    attempt_count,
    last_error_code,
    finished_at
)
ON TABLE public.processing_jobs
FROM zero_to_prod_worker;

REVOKE SELECT (
    id,
    work_item_id,
    state,
    attempt_count,
    last_error_code,
    created_at,
    finished_at
)
ON TABLE public.processing_jobs
FROM zero_to_prod_worker;

REVOKE SELECT (
    id,
    title,
    status,
    created_at
)
ON TABLE public.work_items
FROM zero_to_prod_worker;


REVOKE USAGE
ON SEQUENCE public.outbox_messages_id_seq
FROM zero_to_prod_app;

REVOKE UPDATE (
    publish_attempts,
    last_error_code,
    published_at
)
ON TABLE public.outbox_messages
FROM zero_to_prod_app;

REVOKE INSERT (
    processing_job_id,
    event_type,
    payload
)
ON TABLE public.outbox_messages
FROM zero_to_prod_app;

REVOKE SELECT (
    id,
    processing_job_id,
    event_type,
    payload,
    publish_attempts,
    last_error_code,
    created_at,
    published_at
)
ON TABLE public.outbox_messages
FROM zero_to_prod_app;


REVOKE USAGE
ON SEQUENCE public.processing_jobs_id_seq
FROM zero_to_prod_app;

REVOKE INSERT (work_item_id)
ON TABLE public.processing_jobs
FROM zero_to_prod_app;

REVOKE SELECT (
    id,
    work_item_id,
    state,
    attempt_count,
    last_error_code,
    created_at,
    finished_at
)
ON TABLE public.processing_jobs
FROM zero_to_prod_app;


DROP TABLE public.outbox_messages;
DROP TABLE public.processing_jobs;
