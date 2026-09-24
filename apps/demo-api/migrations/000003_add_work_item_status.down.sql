REVOKE INSERT (status)
ON TABLE public.work_items
FROM zero_to_prod_app;

REVOKE SELECT (status)
ON TABLE public.work_items
FROM zero_to_prod_app;

ALTER TABLE public.work_items
DROP CONSTRAINT work_items_status_check;

ALTER TABLE public.work_items
DROP COLUMN status;
