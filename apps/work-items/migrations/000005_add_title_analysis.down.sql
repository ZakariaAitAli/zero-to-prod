ALTER TABLE public.work_items DROP CONSTRAINT completed_item_result_fk;
ALTER TABLE public.processing_jobs DROP CONSTRAINT successful_job_result_fk;
DROP TABLE public.work_item_results;
ALTER TABLE public.processing_jobs DROP CONSTRAINT processing_jobs_item_identity;
DROP INDEX public.processing_jobs_one_active_or_successful;
REVOKE UPDATE (status) ON public.work_items FROM zero_to_prod_worker;
GRANT INSERT (status) ON public.work_items TO zero_to_prod_app;
ALTER TABLE public.work_items DROP CONSTRAINT work_items_id_status_unique, DROP COLUMN result_work_item_id;
ALTER TABLE public.processing_jobs DROP CONSTRAINT processing_jobs_identity_state_unique, DROP COLUMN result_work_item_id;
