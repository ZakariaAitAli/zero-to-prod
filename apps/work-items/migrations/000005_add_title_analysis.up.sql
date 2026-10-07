DO $$
BEGIN
    IF EXISTS (SELECT FROM public.work_items)
       OR EXISTS (SELECT FROM public.processing_jobs)
       OR EXISTS (SELECT FROM public.outbox_messages) THEN
        RAISE EXCEPTION 'ADR 0003 requires an empty application database; retain legacy data and provision a separate database';
    END IF;
END $$;

CREATE UNIQUE INDEX processing_jobs_one_active_or_successful
ON public.processing_jobs (work_item_id) WHERE state IN ('accepted', 'succeeded');
ALTER TABLE public.processing_jobs ADD CONSTRAINT processing_jobs_item_identity UNIQUE (id, work_item_id);
CREATE TABLE public.work_item_results (
    work_item_id BIGINT PRIMARY KEY REFERENCES public.work_items(id) ON DELETE RESTRICT,
    processing_job_id BIGINT NOT NULL UNIQUE,
    input_title TEXT NOT NULL,
    analysis_version INTEGER NOT NULL CHECK (analysis_version = 1),
    character_count INTEGER NOT NULL CHECK (character_count >= 0),
    word_count INTEGER NOT NULL CHECK (word_count >= 0),
    FOREIGN KEY (processing_job_id, work_item_id)
      REFERENCES public.processing_jobs(id, work_item_id) ON DELETE RESTRICT
);
REVOKE INSERT (status) ON public.work_items FROM zero_to_prod_app;
GRANT UPDATE (status) ON public.work_items TO zero_to_prod_worker;
GRANT SELECT ON public.work_item_results TO zero_to_prod_app, zero_to_prod_worker;
GRANT INSERT ON public.work_item_results TO zero_to_prod_worker;

-- Deferred foreign keys enforce the three-way success invariant at commit,
-- while permitting the worker and pg_restore to write its parts in order.
ALTER TABLE public.work_items
    ADD COLUMN result_work_item_id BIGINT GENERATED ALWAYS AS
      (CASE WHEN status = 'done' THEN id END) STORED,
    ADD CONSTRAINT work_items_id_status_unique UNIQUE (id, status);
ALTER TABLE public.processing_jobs
    ADD COLUMN result_work_item_id BIGINT GENERATED ALWAYS AS
      (CASE WHEN state = 'succeeded' THEN work_item_id END) STORED,
    ADD CONSTRAINT processing_jobs_identity_state_unique UNIQUE (id, work_item_id, state);
ALTER TABLE public.work_item_results
    ADD COLUMN item_status TEXT NOT NULL DEFAULT 'done' CHECK (item_status = 'done'),
    ADD COLUMN job_state TEXT NOT NULL DEFAULT 'succeeded' CHECK (job_state = 'succeeded'),
    ADD CONSTRAINT work_item_results_identity_unique UNIQUE (work_item_id, processing_job_id),
    ADD CONSTRAINT result_completed_item_fk FOREIGN KEY (work_item_id, item_status)
      REFERENCES public.work_items (id, status) DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT result_successful_job_fk FOREIGN KEY (processing_job_id, work_item_id, job_state)
      REFERENCES public.processing_jobs (id, work_item_id, state) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE public.work_items
    ADD CONSTRAINT completed_item_result_fk FOREIGN KEY (result_work_item_id)
      REFERENCES public.work_item_results (work_item_id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE public.processing_jobs
    ADD CONSTRAINT successful_job_result_fk FOREIGN KEY (result_work_item_id, id)
      REFERENCES public.work_item_results (work_item_id, processing_job_id) DEFERRABLE INITIALLY DEFERRED;
