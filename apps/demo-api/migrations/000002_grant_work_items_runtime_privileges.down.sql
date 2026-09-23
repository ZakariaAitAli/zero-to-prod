REVOKE USAGE
ON SEQUENCE public.work_items_id_seq
FROM zero_to_prod_app;

REVOKE INSERT (title)
ON TABLE public.work_items
FROM zero_to_prod_app;

REVOKE SELECT (id, title, created_at)
ON TABLE public.work_items
FROM zero_to_prod_app;
