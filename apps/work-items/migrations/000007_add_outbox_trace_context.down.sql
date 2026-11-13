-- Stop APIs that write trace_context before downgrading.
REVOKE SELECT (trace_context), INSERT (trace_context)
ON TABLE public.outbox_messages
FROM zero_to_prod_app;

ALTER TABLE public.outbox_messages
    DROP CONSTRAINT outbox_messages_trace_context_check,
    DROP COLUMN trace_context;
