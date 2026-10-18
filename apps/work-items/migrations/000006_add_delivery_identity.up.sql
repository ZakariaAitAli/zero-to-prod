-- Deploy with API and workers stopped. Version 1 deliveries are rejected by
-- version 2 workers; accepted jobs must be republished with their new identity.
UPDATE public.outbox_messages AS o
SET payload = jsonb_build_object('type', 'work_item.process', 'version', 2,
        'job_id', j.id, 'work_item_id', j.work_item_id,
        'delivery_id', gen_random_uuid()::text),
    published_at = CASE WHEN j.state = 'accepted' THEN NULL ELSE o.published_at END
FROM public.processing_jobs AS j
WHERE o.processing_job_id = j.id;
GRANT SELECT (processing_job_id, payload) ON public.outbox_messages TO zero_to_prod_worker;
