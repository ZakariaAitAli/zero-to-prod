CREATE TABLE processing_jobs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    work_item_id BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'accepted',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ,

    CONSTRAINT processing_jobs_work_item_fk
        FOREIGN KEY (work_item_id)
        REFERENCES public.work_items (id)
        ON DELETE RESTRICT,

    CONSTRAINT processing_jobs_state_check
        CHECK (state IN ('accepted', 'succeeded', 'failed')),

    CONSTRAINT processing_jobs_attempt_count_check
        CHECK (attempt_count >= 0),

    CONSTRAINT processing_jobs_finished_state_check
        CHECK (
            (
                state = 'accepted'
                AND finished_at IS NULL
            )
            OR
            (
                state IN ('succeeded', 'failed')
                AND finished_at IS NOT NULL
            )
        )
);

CREATE INDEX processing_jobs_work_item_id_idx
ON public.processing_jobs (work_item_id);


CREATE TABLE outbox_messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    processing_job_id BIGINT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ,

    CONSTRAINT outbox_messages_processing_job_fk
        FOREIGN KEY (processing_job_id)
        REFERENCES public.processing_jobs (id)
        ON DELETE RESTRICT,

    CONSTRAINT outbox_messages_processing_job_unique
        UNIQUE (processing_job_id),

    CONSTRAINT outbox_messages_event_type_check
        CHECK (event_type = 'work_item.process'),

    CONSTRAINT outbox_messages_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object'),

    CONSTRAINT outbox_messages_publish_attempts_check
        CHECK (publish_attempts >= 0)
);

CREATE INDEX outbox_messages_unpublished_idx
ON public.outbox_messages (id)
WHERE published_at IS NULL;


GRANT SELECT (
    id,
    work_item_id,
    state,
    attempt_count,
    last_error_code,
    created_at,
    finished_at
)
ON TABLE public.processing_jobs
TO zero_to_prod_app;

GRANT INSERT (work_item_id)
ON TABLE public.processing_jobs
TO zero_to_prod_app;

GRANT USAGE
ON SEQUENCE public.processing_jobs_id_seq
TO zero_to_prod_app;


GRANT SELECT (
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
TO zero_to_prod_app;

GRANT INSERT (
    processing_job_id,
    event_type,
    payload
)
ON TABLE public.outbox_messages
TO zero_to_prod_app;

GRANT UPDATE (
    publish_attempts,
    last_error_code,
    published_at
)
ON TABLE public.outbox_messages
TO zero_to_prod_app;

GRANT USAGE
ON SEQUENCE public.outbox_messages_id_seq
TO zero_to_prod_app;


GRANT SELECT (
    id,
    title,
    status,
    created_at
)
ON TABLE public.work_items
TO zero_to_prod_worker;

GRANT SELECT (
    id,
    work_item_id,
    state,
    attempt_count,
    last_error_code,
    created_at,
    finished_at
)
ON TABLE public.processing_jobs
TO zero_to_prod_worker;

GRANT UPDATE (
    state,
    attempt_count,
    last_error_code,
    finished_at
)
ON TABLE public.processing_jobs
TO zero_to_prod_worker;
