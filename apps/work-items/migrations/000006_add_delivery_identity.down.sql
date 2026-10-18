-- Downgrade with API and workers stopped; republish accepted jobs as version 1.
UPDATE public.outbox_messages AS o
SET payload = payload - 'delivery_id' || jsonb_build_object('version', 1),
    published_at = CASE WHEN j.state = 'accepted' THEN NULL ELSE o.published_at END
FROM public.processing_jobs AS j WHERE o.processing_job_id = j.id;
REVOKE SELECT (processing_job_id, payload) ON public.outbox_messages FROM zero_to_prod_worker;
