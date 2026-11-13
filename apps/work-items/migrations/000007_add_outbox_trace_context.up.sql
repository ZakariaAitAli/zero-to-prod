-- W3C traceparent of the request that accepted the job (ADR 0012). The outbox
-- publisher continues that trace when it publishes, even after a restart.
--
-- Additive and nullable: apply before deploying an API that writes it; an
-- older API ignores the column. Rows without a trace carry NULL, and the check
-- admits only the fixed traceparent format, so no free text is stored.
ALTER TABLE public.outbox_messages
    ADD COLUMN trace_context TEXT NULL,
    ADD CONSTRAINT outbox_messages_trace_context_check
        CHECK (trace_context ~ '^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$');

GRANT SELECT (trace_context), INSERT (trace_context)
ON TABLE public.outbox_messages
TO zero_to_prod_app;
